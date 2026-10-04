package main

// Contacts / chat management / generic call / health check.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/sokel-dev/sokel-official-plugins/feishu/schema"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
)

// opGetUser converts an email/mobile number into an open_id. The batch_get_id endpoint can look
// up several at once, but we only use the single form — "look up one person, then DM them" is
// the main flow on the canvas; batching is left to `call`.
func opGetUser(ctx plugin.Ctx, in *GetUserIn) (*GetUserOut, error) {
	email, mobile := strings.TrimSpace(in.Email), strings.TrimSpace(in.Mobile)
	if email == "" && mobile == "" {
		return nil, fmt.Errorf("邮箱与手机号至少填一个")
	}
	body := map[string]any{}
	if email != "" {
		body["emails"] = []string{email}
	}
	if mobile != "" {
		body["mobiles"] = []string{mobile}
	}
	data, err := callRaw(ctx, credOf(ctx), "POST",
		"/open-apis/contact/v3/users/batch_get_id?user_id_type=open_id", body)
	if err != nil {
		return nil, err
	}
	var r struct {
		UserList []struct {
			UserID string `json:"user_id"`
			Email  string `json:"email"`
			Mobile string `json:"mobile"`
		} `json:"user_list"`
	}
	_ = json.Unmarshal(data, &r)
	for _, u := range r.UserList {
		if u.UserID != "" {
			return &GetUserOut{OpenID: u.UserID}, nil
		}
	}
	return nil, fmt.Errorf("没查到这个用户——邮箱/手机号不在企业通讯录里，或应用缺「通过手机号或邮箱获取用户 ID」权限")
}

func opListChats(ctx plugin.Ctx, in *ListChatsIn) (*ListChatsOut, error) {
	path := "/open-apis/im/v1/chats?page_size=100"
	if t := strings.TrimSpace(in.PageToken); t != "" {
		path += "&page_token=" + t
	}
	data, err := callRaw(ctx, credOf(ctx), "GET", path, nil)
	if err != nil {
		return nil, err
	}
	var r struct {
		Items []struct {
			ChatID      string `json:"chat_id"`
			Name        string `json:"name"`
			Description string `json:"description"`
			OwnerID     string `json:"owner_id"`
			External    bool   `json:"external"`
		} `json:"items"`
		PageToken string `json:"page_token"`
		HasMore   bool   `json:"has_more"`
	}
	_ = json.Unmarshal(data, &r)
	out := &ListChatsOut{PageToken: r.PageToken, HasMore: r.HasMore}
	for _, c := range r.Items {
		out.Chats = append(out.Chats, schema.Chat{ChatID: c.ChatID, Name: c.Name,
			Description: c.Description, OwnerID: c.OwnerID, External: c.External})
	}
	return out, nil
}

func opCreateChat(ctx plugin.Ctx, in *CreateChatIn) (*CreateChatOut, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, fmt.Errorf("群名是空的")
	}
	body := map[string]any{"name": name}
	if d := strings.TrimSpace(in.Description); d != "" {
		body["description"] = d
	}
	if len(in.UserOpenIDs) > 0 {
		body["user_id_list"] = in.UserOpenIDs
	}
	data, err := callRaw(ctx, credOf(ctx), "POST",
		"/open-apis/im/v1/chats?user_id_type=open_id", body)
	if err != nil {
		return nil, err
	}
	var r struct {
		ChatID string `json:"chat_id"`
	}
	_ = json.Unmarshal(data, &r)
	return &CreateChatOut{ChatID: r.ChatID}, nil
}

func opAddChatMembers(ctx plugin.Ctx, in *AddChatMembersIn) (*AddChatMembersOut, error) {
	cid := strings.TrimSpace(in.ChatID)
	if cid == "" {
		return nil, fmt.Errorf("群 ID 是空的")
	}
	if len(in.UserOpenIDs) == 0 {
		return nil, fmt.Errorf("成员列表是空的")
	}
	data, err := callRaw(ctx, credOf(ctx), "POST",
		"/open-apis/im/v1/chats/"+cid+"/members?member_id_type=open_id&succeed_type=1",
		map[string]any{"id_list": in.UserOpenIDs})
	if err != nil {
		return nil, err
	}
	var r struct {
		InvalidIDList []string `json:"invalid_id_list"`
	}
	_ = json.Unmarshal(data, &r)
	return &AddChatMembersOut{InvalidIDs: r.InvalidIDList}, nil
}

// opCall is the generic fallback. **A non-zero code is not turned into an error here**: someone
// using `call` is directly invoking an endpoint from the Feishu docs, and they want the raw
// code/msg to troubleshoot against those docs — wrapping it would just get in the way.
func opCall(ctx plugin.Ctx, in *CallIn) (*CallOut, error) {
	path := strings.TrimSpace(in.Path)
	if !strings.HasPrefix(path, "/open-apis/") {
		return nil, fmt.Errorf("path 要以 /open-apis/ 开头（照抄飞书文档里的接口路径），得到 %q", path)
	}
	method := strings.ToUpper(strings.TrimSpace(in.Method))
	if method == "" {
		method = "POST"
	}
	var body any
	if len(in.Body) > 0 {
		body = in.Body
	}
	data, err := callRawFull(ctx, credOf(ctx), method, path, body)
	if err != nil {
		return nil, err
	}
	out := &CallOut{Code: data.Code, Msg: data.Msg}
	if len(data.Data) > 0 {
		var v any
		_ = json.Unmarshal(data.Data, &v)
		out.Data = v
	}
	return out, nil
}

// opHealthCheck: healthy = **can app_id/app_secret be exchanged for a tenant_access_token**
// (matches the manifest's definition). Two deliberate choices here (issue #13 items 3 and 4):
//   - Hit the token endpoint directly, bypassing the SDK's two-layer client/token cache —
//     otherwise the health check would stay red forever after fixing the secret until a
//     restart, and show a false green for about 2 hours after the secret is revoked; either
//     way the health check wouldn't be reporting the current truth;
//   - bot/v3/info is no longer used as the criterion: an app that hasn't enabled the "Bot"
//     capability (e.g. one that only reads Bitable/docs) would always fail that call even
//     though the credential is perfectly usable. The bot name is now best-effort enrichment —
//     failing to get it doesn't count against the result.
func opHealthCheck(ctx plugin.Ctx, _ *HealthCheckIn) (*HealthCheckOut, error) {
	cred := credOf(ctx)
	appID, secret := strings.TrimSpace(cred.AppID), strings.TrimSpace(cred.AppSecret)
	if appID == "" || secret == "" {
		return &HealthCheckOut{OK: false, Message: "凭证缺 app_id/app_secret（开放平台「凭证与基础信息」页）"}, nil
	}
	body, _ := json.Marshal(map[string]string{"app_id": appID, "app_secret": secret})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		baseURL(cred.Domain)+"/open-apis/auth/v3/tenant_access_token/internal", bytes.NewReader(body))
	if err != nil {
		return &HealthCheckOut{OK: false, Message: err.Error()}, nil
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return &HealthCheckOut{OK: false, Message: "连接飞书失败: " + err.Error()}, nil
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var tk struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if uerr := json.Unmarshal(raw, &tk); uerr != nil {
		return &HealthCheckOut{OK: false, Message: fmt.Sprintf("飞书应答解不开（HTTP %d）", resp.StatusCode)}, nil
	}
	if tk.Code != 0 {
		return &HealthCheckOut{OK: false, Message: fmt.Sprintf("换取 tenant_access_token 失败（code %d）：%s", tk.Code, tk.Msg)}, nil
	}
	// Enrichment: include the bot name if the bot capability is available; not having it
	// doesn't count against the result.
	name := ""
	if data, berr := callRaw(ctx, cred, "GET", "/open-apis/bot/v3/info", nil); berr == nil {
		var r struct {
			Bot struct {
				BotName string `json:"bot_name"`
			} `json:"bot"`
		}
		_ = json.Unmarshal(data, &r)
		name = r.Bot.BotName
	}
	return &HealthCheckOut{OK: true, BotName: name,
		Message: "tenant_access_token 可用" + map[bool]string{true: "，bot：" + name, false: ""}[name != ""]}, nil
}
