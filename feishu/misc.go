package main

// 通讯录 / 群管理 / 通用调用 / 健康检查。

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

// opGetUser 邮箱/手机号 → open_id。batch_get_id 接口一次能查多个，我们只用单个形态——
// 画布上「查一个人再私信」是主流程，批量留给 call。
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

// opCall 通用保底。**这里不把 code!=0 转成错误**：用 call 的人是在直调飞书文档里的
// 接口，他要的是原样的 code/msg 来对照文档排错——包装反而碍事。
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

// opHealthCheck：健康 = **能不能用 app_id/app_secret 换出 tenant_access_token**
// （与 manifest 的口径一致）。两点讲究（issue #13 ③④）：
//   - 直打 token 端点、绕开 SDK 的 client/token 双层缓存——否则改对密钥后体检
//     恒红到重启、密钥被吊销后假绿约 2 小时，体检说的都不是现在的事实；
//   - 不再用 bot/v3/info 当判据：没开「机器人」能力的应用（只读多维表格/云文档
//     那类）调它必失败，而凭证明明可用。bot 名降级为尽力富化，拿不到不扣分。
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
	// 富化：有机器人能力就带上 bot 名；没有也不扣分。
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
