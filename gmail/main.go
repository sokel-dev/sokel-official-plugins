// gmail —— Sokel 第一方插件：读 Gmail + 新邮件触发。
//
// 凭证是**插件自有**的（和别的插件一样归接入组），只是获取方式是 OAuth：
// 用户点一次「授权」→ 同意页 → refresh_token 落平台。插件拿到的只是平台现换的
// access_token，既没有 client_secret 也不经手 refresh_token。
//
// 首发只读（gmail.readonly）：列邮件 / 读单封 / 取附件 + 「收到新邮件」事件。
// 标已读、发信要更大的作用域（gmail.modify / gmail.send），而 Gmail 属 restricted scope，
// 要得越多 Google 的安全评估越难过——等真需要再加。
//
// 运行：SOKEL_ENDPOINT=nats://<broker>:4222 SOKEL_TOKEN=skp_xxx ./gmail
package main

//go:generate go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen

import (
	"encoding/base64"
	"fmt"
	"log"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/sokel-dev/sokel-official-plugins/gmail/schema"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
)

func main() {
	token := sokel.Env("TOKEN")
	if token == "" && sokel.Env("DEPLOY_KEY") == "" {
		log.Fatal("请设置 SOKEL_TOKEN(接入组「接入命令」里复制);随部署托管的容器可改设 SOKEL_DEPLOY_KEY 自动注册")
	}
	p := sokel.New(sokel.Config{
		Endpoint: env("SOKEL_ENDPOINT", "http://localhost:8088"),
		Token:    token,
		Name:     "gmail",
	})

	RegisterCredential(p)  // 凭证契约（schema 声明生成；Cred 类型在 zz_credential.go）
	p.SetDoc(usageDoc, "") // 使用说明（docs/*.md）：凭证怎么拿、有什么坑，随握手上报给平台

	RegisterAuth(p) // 认证方式：Google OAuth（schema 声明；无参数=全程平台代答）

	OnGmailList(p, opList)
	OnGmailGet(p, opGet)
	OnGmailAttachment(p, opAttachment)
	OnHealthCheck(p, opHealthCheck) // 凭证页「检查」按钮调它（id 必须是 health_check）

	DeclareEvents(p)
	// 常驻事件源：增量拉 users.history.list（不重不漏，理由见 history.go）。
	sokel.RegisterSource(p, sokel.Source{ID: "inbox", Label: "新邮件轮询"}, runInboxSource)

	if err := p.Run(); err != nil {
		log.Fatal(err)
	}
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// accessToken：平台注入的短期 token（EnrichOAuthFields 现换后塞进来）。
//
// 取不到就是没授权或授权已失效——两种都该让用户去凭证页点「授权」，所以不区分。
func accessToken(ctx plugin.Ctx) string { return sokel.CredentialAs[Cred](ctx).AccessToken }

// —— 操作 ——

func opList(ctx plugin.Ctx, in *GmailListIn) (*GmailListOut, error) {
	tok := accessToken(ctx)
	var lr listResponse
	if err := gmailGet(ctx, tok, "/messages", listQuery(in.Query, in.LabelIDs, in.MaxResults), &lr); err != nil {
		return nil, err
	}
	items := make([]schema.MessageItem, 0, len(lr.Messages))
	for _, m := range lr.Messages {
		if !in.WithDetail {
			items = append(items, schema.MessageItem{ID: m.ID, ThreadID: m.ThreadID})
			continue
		}
		// 逐封拉详情：慢且更耗配额，所以是显式开关而不是默认行为。
		// 单封失败不中断整批——一封信被删掉不该让整个节点失败。
		var full gmailMessage
		if err := gmailGet(ctx, tok, "/messages/"+url.PathEscape(m.ID), nil, &full); err != nil {
			log.Printf("gmail: 取邮件 %s 详情失败（跳过）: %v", m.ID, err)
			items = append(items, schema.MessageItem{ID: m.ID, ThreadID: m.ThreadID, Error: err.Error()})
			continue
		}
		items = append(items, toDetail(full))
	}
	return &GmailListOut{Messages: items, Count: len(items), NextPageToken: lr.NextPageToken}, nil
}

func opGet(ctx plugin.Ctx, in *GmailGetIn) (*GmailGetOut, error) {
	if in.MessageID == "" {
		return nil, fmt.Errorf("缺少 message_id")
	}
	var m gmailMessage
	if err := gmailGet(ctx, accessToken(ctx), "/messages/"+url.PathEscape(in.MessageID), nil, &m); err != nil {
		return nil, err
	}
	return detailToOut(toDetail(m)), nil
}

// attachmentResponse：附件字节（base64url）。
type attachmentResponse struct {
	Size int    `json:"size"`
	Data string `json:"data"`
}

func opAttachment(ctx plugin.Ctx, in *GmailAttachmentIn) (*GmailAttachmentOut, error) {
	if in.MessageID == "" || in.AttachmentID == "" {
		return nil, fmt.Errorf("缺少 message_id 或 attachment_id")
	}
	var ar attachmentResponse
	path := "/messages/" + url.PathEscape(in.MessageID) + "/attachments/" + url.PathEscape(in.AttachmentID)
	if err := gmailGet(ctx, accessToken(ctx), path, nil, &ar); err != nil {
		return nil, err
	}
	// 附件同样是 base64url（与正文一码事，见 message.go 的 decodeBody）。
	raw, err := base64.URLEncoding.DecodeString(ar.Data)
	if err != nil {
		if raw, err = base64.RawURLEncoding.DecodeString(ar.Data); err != nil {
			return nil, fmt.Errorf("附件内容解码失败: %w", err)
		}
	}
	name := in.Filename
	if name == "" {
		name = "attachment-" + in.AttachmentID[:min(8, len(in.AttachmentID))]
	}
	// 字节经分块通道进平台文件层，出参只给引用——几十 MB 塞进一次调用必炸。
	f, err := ctx.Upload(name, "", raw)
	if err != nil {
		return nil, fmt.Errorf("附件存入文件层失败: %w", err)
	}
	return &GmailAttachmentOut{File: f}, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// opHealthCheck：凭证体检 —— 打一次 users.getProfile。
//
// 选它是因为它是整套 Gmail API 里最便宜的只读调用（不碰任何邮件、不翻页），
// 而且回的是「这个 token 到底代表哪个邮箱」——授权授到了另一个 Google 账号是最常见的一种错，
// 只报「连得上」是看不出来的。
//
// 授权失效时返回 ok=false + message 而**不是** error：平台把 error 当「这个插件没法体检」，
// 把 ok=false 当「体检结论是不可用」，而 apiError 已经把 401 翻成了「请到凭证页重新授权」，
// 那句话正是人排查时唯一有用的东西。
func opHealthCheck(ctx plugin.Ctx, _ *HealthCheckIn) (*HealthCheckOut, error) {
	var prof struct {
		EmailAddress  string `json:"emailAddress"`
		MessagesTotal int    `json:"messagesTotal"`
	}
	if err := gmailGet(ctx, accessToken(ctx), "/profile", nil, &prof); err != nil {
		return &HealthCheckOut{Message: err.Error()}, nil
	}
	return &HealthCheckOut{
		OK: true, Email: prof.EmailAddress, MessagesTotal: prof.MessagesTotal,
		Message: fmt.Sprintf("授权有效（%s，共 %d 封邮件）", prof.EmailAddress, prof.MessagesTotal),
	}, nil
}

// —— 事件源：新邮件 ——

// pollInterval：轮询间隔。Gmail 的配额按「配额单位/秒」算，history.list 很便宜，
// 30s 对「新邮件起工作流」这类场景足够，也不至于把配额吃紧。
const pollInterval = 30 * time.Second

// historyCursorField：游标存回凭证的字段名。
// 只放内存的话，插件一重启要么重推全部、要么漏掉停机期间的信（见 history.go 第 3 条）。
const historyCursorField = "history_id"

func runInboxSource(ctx plugin.SourceCtx) error {
	cursor := sokel.SourceCredentialAs[Cred](ctx).HistoryID
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		next, err := pollOnce(ctx, cursor)
		if err != nil {
			log.Printf("gmail: 轮询失败（%v），%s 后重试", err, pollInterval)
		} else if next != cursor {
			cursor = next
			// 写回凭证：重启后从这里继续。写失败只记日志——本轮已经推出去的事件不能收回，
			// 下轮会从旧游标再来一次（宁可重复也不丢，平台侧对事件有去重）。
			if err := ctx.UpdateCredential(map[string]string{historyCursorField: cursor}); err != nil {
				log.Printf("gmail: 游标写回凭证失败（重启后可能重推）: %v", err)
			}
		}
		select {
		case <-time.After(pollInterval):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// pollOnce：拉一轮增量，推事件，返回新游标。
func pollOnce(ctx plugin.SourceCtx, cursor string) (string, error) {
	tok := sokel.SourceCredentialAs[Cred](ctx).AccessToken

	// 首次启动（没有游标）：**只取当前位置，不推任何历史邮件**。
	// 否则接上一个用了三年的邮箱，工作流会被几万封信瞬间冲垮。
	if cursor == "" {
		var prof struct {
			HistoryID string `json:"historyId"`
		}
		if err := gmailGet(ctx, tok, "/profile", nil, &prof); err != nil {
			return "", err
		}
		log.Printf("gmail: 首次启动，从当前位置 %s 开始（不推历史邮件）", prof.HistoryID)
		return prof.HistoryID, nil
	}

	q := url.Values{}
	q.Set("startHistoryId", cursor)
	q.Set("historyTypes", "messageAdded") // 只要「新到达」，别把标已读也当成新邮件
	var hr historyResponse
	if err := gmailGet(ctx, tok, "/history", q, &hr); err != nil {
		// 404 = 游标太旧被 Gmail 清理了（history 只保留约一周）。
		// 此时无法增量，只能重置到当前位置——重置比"从头把整个邮箱推一遍"强得多。
		if ae, ok := err.(*apiError); ok && ae.Status == 404 {
			log.Printf("gmail: 游标 %s 已过期（Gmail 只保留约一周的 history），重置到当前位置", cursor)
			return "", nil
		}
		return "", err
	}

	ids := sortMessageIDs(newMessageIDs(hr.History))
	for _, id := range ids {
		var m gmailMessage
		if err := gmailGet(ctx, tok, "/messages/"+url.PathEscape(id), nil, &m); err != nil {
			log.Printf("gmail: 取邮件 %s 失败（跳过本封）: %v", id, err)
			continue
		}
		ev := &MessageReceivedEvent{
			MessageID: m.ID, ThreadID: m.ThreadID,
			Subject: header(m.Payload, "Subject"),
			From:    header(m.Payload, "From"),
			To:      header(m.Payload, "To"),
			Date:    header(m.Payload, "Date"),
			Snippet: m.Snippet, HasAttachments: hasAttachments(m.Payload),
			LabelIDs: m.LabelIDs,
		}
		// 事件 id 用邮件 id：平台按它去重，所以重复推同一封是安全的
		// （游标写回失败时下轮会重来一遍，靠这个兜住）。
		if err := TriggerMessageReceived(ctx, m.ID, ev); err != nil {
			log.Printf("gmail: 推事件失败 %s: %v", m.ID, err)
		}
	}
	// 游标取这批里的最大值，而不是应答里的 historyId——后者是"当前最新"，
	// 若本轮有邮件因取详情失败被跳过，直接跳到最新就等于永久丢掉它们。
	return maxHistoryID(hr.History, cursor), nil
}

// detailToOut：元素类型 → 「读邮件」的**平铺**出参。
//
// 两种形状都要：列表里是元素（下游 messages[0].subject），单封读取则平铺到顶层
// （下游 gmail.subject），后者少一层引用路径。
func detailToOut(d schema.MessageItem) *GmailGetOut {
	out := &GmailGetOut{
		ID: d.ID, ThreadID: d.ThreadID, Subject: d.Subject,
		From: d.From, To: d.To, Date: d.Date,
		Text: d.Text, HTML: d.HTML, Snippet: d.Snippet,
		LabelIDs: d.LabelIDs,
	}
	out.Attachments = append(out.Attachments, d.Attachments...)
	return out
}

var _ = strconv.Itoa // 保留：分页/数值参数扩展时用
