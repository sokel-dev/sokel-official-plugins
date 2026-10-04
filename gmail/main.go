// gmail — Sokel's first-party plugin: read Gmail + trigger on new messages.
//
// The credential is **owned by the plugin** (belongs to an access group like any other plugin),
// but it's obtained via OAuth: the user clicks "authorize" once -> consent screen ->
// refresh_token lands on the platform. All the plugin gets is the access_token the platform
// just exchanged; it never has a client_secret and never handles a refresh_token.
//
// Read-only for the first release (gmail.readonly): list messages / read a single message / get
// attachments, plus a "new message received" event. Marking as read and sending mail would need
// broader scopes (gmail.modify / gmail.send), and Gmail scopes are restricted — the more you
// request, the harder Google's security review gets — add them when actually needed.
//
// Run: SOKEL_ENDPOINT=nats://<broker>:4222 SOKEL_TOKEN=skp_xxx ./gmail
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

	RegisterCredential(p)  // credential contract (generated from schema declarations; the Cred type is in zz_credential.go)
	p.SetDoc(usageDoc, "") // usage docs (docs/*.md): how to get a credential, what the pitfalls are; reported to the platform during the handshake

	RegisterAuth(p) // auth method: Google OAuth (schema declaration; no params = the platform handles the whole flow)

	OnGmailList(p, opList)
	OnGmailGet(p, opGet)
	OnGmailAttachment(p, opAttachment)
	OnHealthCheck(p, opHealthCheck) // called by the "check" button on the credential page (the id must be health_check)

	DeclareEvents(p)
	// Persistent event source: pulls users.history.list incrementally (no duplicates, no
	// gaps — see history.go for why).
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

// accessToken returns the short-lived token injected by the platform (filled in by
// EnrichOAuthFields right after it exchanges one).
//
// If it can't be obtained, it's either "never authorized" or "authorization expired" — both
// cases should send the user to click "authorize" on the credential page, so they aren't
// distinguished.
func accessToken(ctx plugin.Ctx) string { return sokel.CredentialAs[Cred](ctx).AccessToken }

// —— Operations ——

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
		// Fetching detail per message is slow and uses more quota, so it's an explicit
		// opt-in rather than the default. A single failure doesn't abort the whole batch —
		// one deleted message shouldn't fail the entire node.
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

// attachmentResponse holds the attachment bytes (base64url).
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
	// Attachments are also base64url (same deal as the body, see decodeBody in message.go).
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
	// Bytes go into the platform's file layer through a chunked channel; the output only
	// gives a reference — stuffing tens of MB into a single call is bound to blow up.
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

// opHealthCheck checks the credential by making one call to users.getProfile.
//
// It's chosen because it's the cheapest read-only call in the whole Gmail API (touches no
// messages, no pagination), and it returns "which mailbox does this token actually represent" —
// the authorization being granted to the wrong Google account is one of the most common
// mistakes, and simply reporting "connection OK" wouldn't catch it.
//
// When authorization has expired, this returns ok=false + message, **not** an error: the
// platform treats an error as "this plugin can't run a health check" and ok=false as "the
// health check concluded it's unavailable," and apiError has already translated a 401 into
// "please re-authorize on the credential page" — that message is exactly what's useful to a
// person troubleshooting this.
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

// —— Event source: new messages ——

// pollInterval is the polling interval. Gmail's quota is measured in "quota units per second,"
// and history.list is cheap; 30s is enough for "start a workflow on new mail" use cases without
// putting much pressure on the quota.
const pollInterval = 30 * time.Second

// historyCursorField is the field name the cursor is persisted to in the credential.
// Keeping it only in memory would mean the plugin either re-pushes everything or misses
// messages that arrived while it was down, on every restart (see point 3 in history.go).
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
			// Persist to the credential: resumes from here after a restart. A write
			// failure is only logged — the events already pushed this round can't be
			// taken back, and the next round will redo it from the old cursor
			// (duplicates are fine, loss isn't; the platform dedups events).
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

// pollOnce fetches one round of incremental changes, pushes events, and returns the new cursor.
func pollOnce(ctx plugin.SourceCtx, cursor string) (string, error) {
	tok := sokel.SourceCredentialAs[Cred](ctx).AccessToken

	// First run (no cursor): **only record the current position, push no historical
	// messages at all**. Otherwise connecting a mailbox that's been in use for three years
	// would flood the workflow with tens of thousands of messages instantly.
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
	q.Set("historyTypes", "messageAdded") // only want "newly arrived," don't treat marking-as-read as a new message
	var hr historyResponse
	if err := gmailGet(ctx, tok, "/history", q, &hr); err != nil {
		// 404 = the cursor is too old and was cleaned up by Gmail (history is kept for
		// only about a week). Incremental fetching isn't possible at that point, so reset
		// to the current position instead — resetting beats "pushing the whole mailbox
		// from scratch" by a wide margin.
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
		// The event id is the message id: the platform dedups by it, so pushing the same
		// message twice is safe (this is what backstops a cursor write failure causing
		// the next round to redo it).
		if err := TriggerMessageReceived(ctx, m.ID, ev); err != nil {
			log.Printf("gmail: 推事件失败 %s: %v", m.ID, err)
		}
	}
	// The cursor takes the max value in this batch, not the response's historyId — the
	// latter is "current latest," and if any message was skipped this round due to a
	// detail-fetch failure, jumping straight to the latest would lose it permanently.
	return maxHistoryID(hr.History, cursor), nil
}

// detailToOut converts the element type into the **flattened** output of "read message."
//
// Both shapes are needed: in a list it's an element (downstream references messages[0].subject),
// while reading a single message flattens it to the top level (downstream references
// gmail.subject) — the latter removes one level from the reference path.
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

var _ = strconv.Itoa // kept: for future pagination/numeric parameter extensions
