package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"
)

// AppMessageDelivery preserves the response_code used to update template cards.
// API acceptance does not imply rendering in the ordinary WeChat client.
type AppMessageDelivery struct {
	Delivery
	ResponseCode string `json:"response_code"`
}

// apiObject keeps the documented, variant-dependent nested JSON intact. Duplicate
// root keys are rejected instead of letting a parser silently choose a value.
func apiObject(raw json.RawMessage) (map[string]json.RawMessage, error) {
	if len(raw) == 0 || len(raw) > 1024*1024 || !utf8.Valid(raw) {
		return nil, errors.New("JSON 对象不能为空，且 UTF-8 长度不能超过 1 MiB")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	first, err := d.Token()
	if err != nil || first != json.Delim('{') {
		return nil, errors.New("参数必须是有效的 JSON 对象")
	}
	object := make(map[string]json.RawMessage)
	for d.More() {
		token, err := d.Token()
		if err != nil {
			return nil, errors.New("参数不是有效的 JSON 对象")
		}
		key, ok := token.(string)
		if !ok {
			return nil, errors.New("JSON 对象字段名无效")
		}
		if _, exists := object[key]; exists {
			return nil, errors.New("JSON 对象不能包含重复字段")
		}
		var value json.RawMessage
		if err := d.Decode(&value); err != nil {
			return nil, errors.New("参数不是有效的 JSON 对象")
		}
		object[key] = value
	}
	if last, err := d.Token(); err != nil || last != json.Delim('}') {
		return nil, errors.New("参数不是有效的 JSON 对象")
	}
	if _, err := d.Token(); err != io.EOF {
		return nil, errors.New("JSON 对象后不能附加其他内容")
	}
	return object, nil
}

func apiAllowedFields(object map[string]json.RawMessage, fields ...string) error {
	for key := range object {
		allowed := false
		for _, field := range fields {
			if key == field {
				allowed = true
				break
			}
		}
		if !allowed {
			// Do not echo arbitrary caller-supplied field names into errors/logs.
			return errors.New("JSON 包含不支持的字段；应用、消息类型及收件人须使用独立参数，不能在 JSON 中覆盖")
		}
	}
	return nil
}

func apiString(raw json.RawMessage) (string, error) {
	var s *string
	if err := json.Unmarshal(raw, &s); err != nil || s == nil {
		return "", errors.New("字段必须是字符串")
	}
	return *s, nil
}

func apiInt(raw json.RawMessage, minValue, maxValue int) error {
	var value *int
	if err := json.Unmarshal(raw, &value); err != nil || value == nil || *value < minValue || *value > maxValue {
		return fmt.Errorf("整数字段必须在 %d 至 %d 之间", minValue, maxValue)
	}
	return nil
}

func apiHTTPURL(value string, maxBytes int) bool {
	u, err := url.Parse(value)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Hostname() != "" && u.User == nil && len(value) <= maxBytes
}

func apiRawString(value string) json.RawMessage {
	b, _ := json.Marshal(value)
	return b
}

// SendAppMessage implements document/path/90236. body contains the selected
// message object and optional send flags, e.g. {"image":{"media_id":"..."}}.
// Target selection is deliberately limited to explicit local member IDs.
func (c *WeComClient) SendAppMessage(ctx context.Context, cred Cred, users []string, msgType string, body json.RawMessage) (AppMessageDelivery, error) {
	var result AppMessageDelivery
	cred, agentID, err := normalizedWeComCred(cred)
	if err != nil {
		return result, err
	}
	targets, err := weComRecipients(users)
	if err != nil {
		return result, err
	}
	switch msgType {
	case "text", "image", "voice", "video", "file", "textcard", "news", "mpnews", "markdown", "miniprogram_notice", "template_card":
	default:
		return result, errors.New("不支持的应用消息类型")
	}
	object, err := apiObject(body)
	if err != nil {
		return result, err
	}
	if err := apiAllowedFields(object, msgType, "safe", "enable_id_trans", "enable_duplicate_check", "duplicate_check_interval"); err != nil {
		return result, err
	}
	if _, err := apiObject(object[msgType]); err != nil {
		return result, errors.New("JSON 必须包含与消息类型同名的消息内容对象")
	}
	for _, key := range []string{"safe", "enable_id_trans", "enable_duplicate_check", "duplicate_check_interval"} {
		if value, ok := object[key]; ok {
			minValue, maxValue := 0, 1
			if key == "safe" && msgType == "mpnews" {
				maxValue = 2
			}
			if key == "duplicate_check_interval" {
				minValue, maxValue = 1, 14400
			}
			if err := apiInt(value, minValue, maxValue); err != nil {
				return result, fmt.Errorf("发送选项 %s：%w", key, err)
			}
		}
	}
	object["touser"] = apiRawString(targets)
	object["agentid"] = json.RawMessage(strconv.FormatInt(agentID, 10))
	object["msgtype"] = apiRawString(msgType)
	b, _ := json.Marshal(object)
	b, err = c.authenticated(ctx, cred, http.MethodPost, "/cgi-bin/message/send", nil, b)
	if err != nil {
		return result, err
	}
	var out struct {
		MsgID          string `json:"msgid"`
		InvalidUser    string `json:"invaliduser"`
		InvalidParty   string `json:"invalidparty"`
		InvalidTag     string `json:"invalidtag"`
		UnlicensedUser string `json:"unlicenseduser"`
		ResponseCode   string `json:"response_code"`
	}
	if err := json.Unmarshal(b, &out); err != nil || strings.TrimSpace(out.MsgID) == "" {
		return result, errors.New("企业微信发送响应缺少有效 msgid 或字段格式错误；发送结果未知")
	}
	return AppMessageDelivery{Delivery: Delivery{out.MsgID, out.InvalidUser, out.InvalidParty, out.InvalidTag, out.UnlicensedUser}, ResponseCode: out.ResponseCode}, nil
}

// RecallMessage only recalls messages sent within 24 hours in the WeCom client.
// Ordinary WeChat / 微信插件 copies cannot be recalled (document/path/94867).
func (c *WeComClient) RecallMessage(ctx context.Context, cred Cred, msgID string) error {
	if strings.TrimSpace(msgID) == "" || len(msgID) > 1024 || !utf8.ValidString(msgID) {
		return errors.New("消息 ID 不能为空，且不能超过 1024 字节")
	}
	body, _ := json.Marshal(struct {
		MsgID string `json:"msgid"`
	}{msgID})
	_, err := c.authenticated(ctx, cred, http.MethodPost, "/cgi-bin/message/recall", nil, body)
	return err
}

// GetApplication returns the current credential's application, never an
// arbitrary caller-selected AgentID (document/path/90227).
func (c *WeComClient) GetApplication(ctx context.Context, cred Cred) (json.RawMessage, error) {
	cred, agentID, err := normalizedWeComCred(cred)
	if err != nil {
		return nil, err
	}
	b, err := c.authenticated(ctx, cred, http.MethodGet, "/cgi-bin/agent/get", url.Values{"agentid": {strconv.FormatInt(agentID, 10)}}, nil)
	if err != nil {
		return nil, err
	}
	var out struct {
		AgentID *int64 `json:"agentid"`
	}
	if err := json.Unmarshal(b, &out); err != nil || out.AgentID == nil || *out.AgentID != agentID {
		return nil, errors.New("企业微信返回的应用 AgentID 与凭证不一致")
	}
	return json.RawMessage(b), nil
}

// SetApplication patches documented application settings. Absent fields remain
// absent so that changing a homepage does not reset other settings (/90228).
func (c *WeComClient) SetApplication(ctx context.Context, cred Cred, patch json.RawMessage) error {
	object, err := apiObject(patch)
	if err != nil {
		return err
	}
	if len(object) == 0 {
		return errors.New("至少提供一个应用设置字段")
	}
	if err := apiAllowedFields(object, "report_location_flag", "logo_mediaid", "name", "description", "redirect_domain", "isreportenter", "home_url"); err != nil {
		return err
	}
	for field, raw := range object {
		if field == "report_location_flag" || field == "isreportenter" {
			if err := apiInt(raw, 0, 1); err != nil {
				return err
			}
			continue
		}
		value, err := apiString(raw)
		if err != nil {
			return err
		}
		switch field {
		case "name":
			if strings.TrimSpace(value) == "" || utf8.RuneCountInString(value) > 32 {
				return errors.New("应用名称不能为空，且不能超过 32 个字符")
			}
		case "description":
			if n := utf8.RuneCountInString(value); n < 4 || n > 120 {
				return errors.New("应用详情必须为 4 至 120 个字符")
			}
		case "home_url":
			if !apiHTTPURL(value, 2048) {
				return errors.New("应用主页必须是有效的 HTTP(S) 地址，且不能超过 2048 字节")
			}
		case "redirect_domain":
			if strings.TrimSpace(value) == "" || strings.ContainsAny(value, "/?#:@ \\ \t\r\n") || len(value) > 253 {
				return errors.New("可信域名必须填写不含协议和路径的域名")
			}
		case "logo_mediaid":
			if strings.TrimSpace(value) == "" || len(value) > 1024 {
				return errors.New("应用图标 media_id 不能为空，且不能超过 1024 字节")
			}
		}
	}
	_, err = c.applicationPost(ctx, cred, "/cgi-bin/agent/set", object)
	return err
}

func (c *WeComClient) GetMenu(ctx context.Context, cred Cred) (json.RawMessage, error) {
	return c.menuRequest(ctx, cred, http.MethodGet, "/cgi-bin/menu/get", nil)
}

func (c *WeComClient) SetMenu(ctx context.Context, cred Cred, menu json.RawMessage) error {
	object, err := apiObject(menu)
	if err != nil {
		return err
	}
	if err := apiAllowedFields(object, "button"); err != nil {
		return err
	}
	if err := validateMenuButtons(object["button"], false); err != nil {
		return err
	}
	_, err = c.menuRequest(ctx, cred, http.MethodPost, "/cgi-bin/menu/create", menu)
	return err
}

func (c *WeComClient) DeleteMenu(ctx context.Context, cred Cred) error {
	_, err := c.menuRequest(ctx, cred, http.MethodGet, "/cgi-bin/menu/delete", nil)
	return err
}

func (c *WeComClient) menuRequest(ctx context.Context, cred Cred, method, path string, body []byte) (json.RawMessage, error) {
	cred, agentID, err := normalizedWeComCred(cred)
	if err != nil {
		return nil, err
	}
	b, err := c.authenticated(ctx, cred, method, path, url.Values{"agentid": {strconv.FormatInt(agentID, 10)}}, body)
	return json.RawMessage(b), err
}

func validateMenuButtons(raw json.RawMessage, child bool) error {
	var buttons []json.RawMessage
	limit, nameLimit := 3, 16
	if child {
		limit, nameLimit = 5, 40
	}
	if err := json.Unmarshal(raw, &buttons); err != nil || len(buttons) == 0 || len(buttons) > limit {
		return fmt.Errorf("菜单按钮数量必须为 1 至 %d", limit)
	}
	for _, rawButton := range buttons {
		button, err := apiObject(rawButton)
		if err != nil {
			return err
		}
		if err := apiAllowedFields(button, "name", "type", "key", "url", "appid", "pagepath", "sub_button"); err != nil {
			return err
		}
		name, err := apiString(button["name"])
		if err != nil || strings.TrimSpace(name) == "" || len(name) > nameLimit {
			return fmt.Errorf("菜单名称不能为空，且不能超过 %d 字节", nameLimit)
		}
		if sub, ok := button["sub_button"]; ok {
			var entries []json.RawMessage
			if err := json.Unmarshal(sub, &entries); err != nil || bytes.Equal(bytes.TrimSpace(sub), []byte("null")) {
				return errors.New("sub_button 必须是数组")
			}
			if len(entries) > 0 {
				if child {
					return errors.New("自定义菜单最多支持两级")
				}
				if err := apiAllowedFields(button, "name", "sub_button"); err != nil {
					return errors.New("有子菜单的父菜单不能同时配置响应动作")
				}
				if err := validateMenuButtons(sub, true); err != nil {
					return err
				}
				continue
			}
		}
		kind, err := apiString(button["type"])
		if err != nil {
			return errors.New("菜单缺少响应动作类型")
		}
		switch kind {
		case "click", "scancode_push", "scancode_waitmsg", "pic_sysphoto", "pic_photo_or_album", "pic_weixin", "location_select":
			key, err := apiString(button["key"])
			if err != nil || strings.TrimSpace(key) == "" || len(key) > 128 {
				return errors.New("事件菜单 key 不能为空，且不能超过 128 字节")
			}
		case "view":
			link, err := apiString(button["url"])
			if err != nil || !apiHTTPURL(link, 1024) {
				return errors.New("网页菜单 url 必须是有效的 HTTP(S) 地址，且不能超过 1024 字节")
			}
		case "view_miniprogram":
			appid, appErr := apiString(button["appid"])
			page, pageErr := apiString(button["pagepath"])
			if appErr != nil || pageErr != nil || strings.TrimSpace(appid) == "" || strings.TrimSpace(page) == "" {
				return errors.New("小程序菜单必须包含 appid 和 pagepath")
			}
		default:
			return errors.New("不支持的菜单响应动作类型")
		}
	}
	return nil
}

// Workbench APIs are POST, including reads. See document/path/92535.
func (c *WeComClient) GetWorkbenchTemplate(ctx context.Context, cred Cred) (json.RawMessage, error) {
	return c.applicationPost(ctx, cred, "/cgi-bin/agent/get_workbench_template", make(map[string]json.RawMessage))
}

func (c *WeComClient) SetWorkbenchTemplate(ctx context.Context, cred Cred, body json.RawMessage) error {
	object, err := workbenchObject(body, true)
	if err != nil {
		return err
	}
	_, err = c.applicationPost(ctx, cred, "/cgi-bin/agent/set_workbench_template", object)
	return err
}

func (c *WeComClient) GetWorkbenchData(ctx context.Context, cred Cred, userID string) (json.RawMessage, error) {
	if _, err := weComRecipients([]string{userID}); err != nil {
		return nil, err
	}
	return c.applicationPost(ctx, cred, "/cgi-bin/agent/get_workbench_data", map[string]json.RawMessage{"userid": apiRawString(userID)})
}

func (c *WeComClient) SetWorkbenchData(ctx context.Context, cred Cred, userID string, body json.RawMessage) error {
	if _, err := weComRecipients([]string{userID}); err != nil {
		return err
	}
	object, err := workbenchObject(body, false)
	if err != nil {
		return err
	}
	object["userid"] = apiRawString(userID)
	_, err = c.applicationPost(ctx, cred, "/cgi-bin/agent/set_workbench_data", object)
	return err
}

func (c *WeComClient) BatchSetWorkbenchData(ctx context.Context, cred Cred, users []string, body json.RawMessage) error {
	if _, err := weComRecipients(users); err != nil {
		return err
	}
	if _, err := workbenchObject(body, false); err != nil {
		return err
	}
	userList, _ := json.Marshal(users)
	_, err := c.applicationPost(ctx, cred, "/cgi-bin/agent/batch_set_workbench_data", map[string]json.RawMessage{"userid_list": userList, "data": body})
	return err
}

func workbenchObject(body json.RawMessage, template bool) (map[string]json.RawMessage, error) {
	object, err := apiObject(body)
	if err != nil {
		return nil, err
	}
	kind, err := apiString(object["type"])
	if err != nil {
		return nil, errors.New("工作台数据必须包含 type 字符串")
	}
	switch kind {
	case "keydata", "image", "list", "webview":
	case "normal":
		if !template {
			return nil, errors.New("用户工作台数据不支持 normal 类型")
		}
	default:
		return nil, errors.New("不支持的工作台展示类型")
	}
	allowed := []string{"type"}
	if kind != "normal" {
		allowed = append(allowed, kind)
	}
	if template {
		allowed = append(allowed, "replace_user_data")
	}
	if err := apiAllowedFields(object, allowed...); err != nil {
		return nil, err
	}
	if raw, exists := object["replace_user_data"]; exists {
		var replace *bool
		if err := json.Unmarshal(raw, &replace); err != nil || replace == nil {
			return nil, errors.New("replace_user_data 必须是布尔值")
		}
	}
	if raw, exists := object[kind]; exists || !template {
		if _, err := apiObject(raw); err != nil {
			return nil, errors.New("工作台数据必须包含与 type 同名的 JSON 对象")
		}
	}
	return object, nil
}

func (c *WeComClient) applicationPost(ctx context.Context, cred Cred, path string, object map[string]json.RawMessage) (json.RawMessage, error) {
	cred, agentID, err := normalizedWeComCred(cred)
	if err != nil {
		return nil, err
	}
	// Only internal validated call sites reach this function. Always replace the
	// application identity from the credential immediately before serialization.
	object["agentid"] = json.RawMessage(strconv.FormatInt(agentID, 10))
	body, err := json.Marshal(object)
	if err != nil {
		return nil, errors.New("无法编码企业微信应用设置")
	}
	b, err := c.authenticated(ctx, cred, http.MethodPost, path, nil, body)
	return json.RawMessage(b), err
}
