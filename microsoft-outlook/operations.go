package main

import (
	"context"
	"fmt"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const mailFields = "id,subject,from,toRecipients,receivedDateTime,body,bodyPreview,isRead,hasAttachments,webLink,internetMessageId"
const attachmentFields = "id,name,contentType,size,isInline"
const eventFields = "id,subject,bodyPreview,start,end,location,webLink,isCancelled,isAllDay"

type emailAddress struct {
	Name    string `json:"name,omitempty"`
	Address string `json:"address"`
}
type recipient struct {
	EmailAddress emailAddress `json:"emailAddress"`
}
type itemBody struct {
	ContentType string `json:"contentType"`
	Content     string `json:"content"`
}
type rawMail struct {
	ID, Subject                string
	From                       recipient
	ToRecipients               []recipient
	ReceivedDateTime           string
	Body                       itemBody
	BodyPreview                string
	IsRead, HasAttachments     bool
	WebLink, InternetMessageID string
}

func (r rawMail) typed() MailMessage {
	to := []string{}
	for _, x := range r.ToRecipients {
		to = append(to, x.EmailAddress.Address)
	}
	return MailMessage{ID: r.ID, Subject: r.Subject, Sender: r.From.EmailAddress.Address, To: to, ReceivedAt: r.ReceivedDateTime, Body: r.Body.Content, BodyType: r.Body.ContentType, BodyPreview: r.BodyPreview, IsRead: r.IsRead, HasAttachments: r.HasAttachments, WebURL: r.WebLink, InternetMessageID: r.InternetMessageID}
}
func (a *api) list_messages(ctx context.Context, in *ListMessagesIn) (*ListMessagesOut, error) {
	n, e := pageSize(in.Limit)
	if e != nil {
		return nil, e
	}
	suffix := "/messages"
	if in.FolderID != "" {
		id, e := segment(in.FolderID, "folder_id")
		if e != nil {
			return nil, e
		}
		suffix = "/mailFolders/" + id + "/messages"
	}
	base, e := a.delegated(ctx)
	if e != nil {
		return nil, e
	}
	q := url.Values{"$top": {strconv.Itoa(n)}, "$select": {mailFields}}
	if in.Filter != "" {
		q.Set("$filter", in.Filter)
	}
	var values []rawMail
	next, e := a.page(ctx, base+suffix, q, in.NextLink, &values)
	if e != nil {
		return nil, e
	}
	out := &ListMessagesOut{Messages: []MailMessage{}, NextLink: next}
	for _, v := range values {
		if v.ID == "" {
			return nil, fmt.Errorf("Microsoft Graph returned a mail item without its ID")
		}
		out.Messages = append(out.Messages, v.typed())
	}
	return out, nil
}
func (a *api) get_message(ctx context.Context, in *GetMessageIn) (*GetMessageOut, error) {
	id, e := segment(in.MessageID, "message_id")
	if e != nil {
		return nil, e
	}
	base, e := a.delegated(ctx)
	if e != nil {
		return nil, e
	}
	var r rawMail
	if e = a.json(ctx, "GET", base+"/messages/"+id, url.Values{"$select": {mailFields}}, nil, &r, 200); e != nil {
		return nil, e
	}
	if r.ID == "" {
		return nil, fmt.Errorf("Microsoft Graph returned a mail item without its ID")
	}
	return &GetMessageOut{Message: r.typed()}, nil
}
func recipients(addresses []string) ([]recipient, error) {
	out := []recipient{}
	for _, s := range addresses {
		v, e := mail.ParseAddress(s)
		if e != nil || v.Address != s || strings.ContainsAny(s, "\r\n") {
			return nil, fmt.Errorf("recipient addresses must be plain valid email addresses")
		}
		out = append(out, recipient{EmailAddress: emailAddress{Address: s}})
	}
	return out, nil
}
func (a *api) send_mail(ctx context.Context, in *SendMailIn) (*SendMailOut, error) {
	if len(in.To) == 0 || len(in.To)+len(in.Cc)+len(in.Bcc) > 100 {
		return nil, fmt.Errorf("provide at least one To address and at most 100 total recipients")
	}
	if strings.TrimSpace(in.Subject) == "" || in.Body == "" {
		return nil, fmt.Errorf("subject and body are required")
	}
	kind, e := contentType(in.BodyType)
	if e != nil {
		return nil, e
	}
	to, e := recipients(in.To)
	if e != nil {
		return nil, e
	}
	cc, e := recipients(in.Cc)
	if e != nil {
		return nil, e
	}
	bcc, e := recipients(in.Bcc)
	if e != nil {
		return nil, e
	}
	base, e := a.delegated(ctx)
	if e != nil {
		return nil, e
	}
	payload := map[string]any{"message": map[string]any{"subject": in.Subject, "body": itemBody{strings.ToUpper(kind[:1]) + kind[1:], in.Body}, "toRecipients": to, "ccRecipients": cc, "bccRecipients": bcc}}
	if kind == "html" {
		payload["message"].(map[string]any)["body"] = itemBody{"HTML", in.Body}
	}
	if e = a.json(ctx, "POST", base+"/sendMail", nil, payload, nil, 202); e != nil {
		return nil, e
	}
	return &SendMailOut{Accepted: true, Message: "Microsoft Graph accepted the message; delivery is asynchronous and not confirmed"}, nil
}

type rawAttachment struct {
	Type                  string `json:"@odata.type"`
	ID, Name, ContentType string
	Size                  int
	IsInline              bool
}

func (r rawAttachment) typed() MailAttachment {
	return MailAttachment{ID: r.ID, Name: r.Name, ContentType: r.ContentType, Size: r.Size, IsInline: r.IsInline, AttachmentType: r.Type}
}
func (a *api) list_attachments(ctx context.Context, in *ListAttachmentsIn) (*ListAttachmentsOut, error) {
	id, e := segment(in.MessageID, "message_id")
	if e != nil {
		return nil, e
	}
	base, e := a.delegated(ctx)
	if e != nil {
		return nil, e
	}
	var values []rawAttachment
	next, e := a.page(ctx, base+"/messages/"+id+"/attachments", url.Values{"$select": {attachmentFields}}, in.NextLink, &values)
	if e != nil {
		return nil, e
	}
	out := &ListAttachmentsOut{Attachments: []MailAttachment{}, NextLink: next}
	for _, v := range values {
		if v.ID == "" {
			return nil, fmt.Errorf("Microsoft Graph returned an attachment without its ID")
		}
		out.Attachments = append(out.Attachments, v.typed())
	}
	return out, nil
}
func (a *api) download_attachment(ctx plugin.Ctx, in *DownloadAttachmentIn) (*DownloadAttachmentOut, error) {
	m, e := segment(in.MessageID, "message_id")
	if e != nil {
		return nil, e
	}
	id, e := segment(in.AttachmentID, "attachment_id")
	if e != nil {
		return nil, e
	}
	base, e := a.delegated(ctx)
	if e != nil {
		return nil, e
	}
	path := base + "/messages/" + m + "/attachments/" + id
	var meta rawAttachment
	if e = a.json(ctx, "GET", path, url.Values{"$select": {attachmentFields}}, nil, &meta, 200); e != nil {
		return nil, e
	}
	if meta.Type != "#microsoft.graph.fileAttachment" {
		return nil, fmt.Errorf("only file attachments can be downloaded; item and reference attachments are not supported")
	}
	if meta.ID == "" || meta.Size < 0 || meta.Size > jsonLimit {
		return nil, fmt.Errorf("attachment metadata is missing or exceeds the 16 MiB download limit")
	}
	b, e := a.request(ctx, "GET", path+"/$value", nil, nil, 200)
	if e != nil {
		return nil, e
	}
	name := strings.NewReplacer("/", "_", "\\", "_", "\r", "_", "\n", "_", "\x00", "_").Replace(meta.Name)
	if name == "" || name == "." || name == ".." {
		name = "attachment"
	}
	mime := meta.ContentType
	if mime == "" {
		mime = "application/octet-stream"
	}
	file, e := ctx.Upload(name, mime, b)
	if e != nil {
		return nil, fmt.Errorf("failed to store the downloaded attachment in Sokel")
	}
	if file == nil {
		return nil, fmt.Errorf("Sokel did not return the downloaded file")
	}
	return &DownloadAttachmentOut{File: file}, nil
}

type dateTimeZone struct {
	DateTime string `json:"dateTime"`
	TimeZone string `json:"timeZone"`
}
type rawEvent struct {
	ID, Subject, BodyPreview string
	Start, End               dateTimeZone
	Location                 struct{ DisplayName string }
	WebLink                  string
	IsCancelled, IsAllDay    bool
}

func (r rawEvent) typed() CalendarEvent {
	return CalendarEvent{ID: r.ID, Subject: r.Subject, BodyPreview: r.BodyPreview, Start: r.Start.DateTime, StartTimezone: r.Start.TimeZone, End: r.End.DateTime, EndTimezone: r.End.TimeZone, Location: r.Location.DisplayName, WebURL: r.WebLink, IsCancelled: r.IsCancelled, IsAllDay: r.IsAllDay}
}
func timeRange(start, end string) (time.Time, time.Time, error) {
	s, e := time.Parse(time.RFC3339Nano, start)
	if e != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("start must be RFC3339 with an explicit timezone offset")
	}
	f, e := time.Parse(time.RFC3339Nano, end)
	if e != nil || !f.After(s) {
		return time.Time{}, time.Time{}, fmt.Errorf("end must be RFC3339 with an explicit timezone offset and later than start")
	}
	return s.UTC(), f.UTC(), nil
}
func calendarSuffix(id string) (string, error) {
	if id == "" {
		return "/calendar", nil
	}
	v, e := segment(id, "calendar_id")
	if e != nil {
		return "", e
	}
	return "/calendars/" + v, nil
}
func (a *api) calendar_view(ctx context.Context, in *CalendarViewIn) (*CalendarViewOut, error) {
	s, f, e := timeRange(in.Start, in.End)
	if e != nil {
		return nil, e
	}
	n, e := pageSize(in.Limit)
	if e != nil {
		return nil, e
	}
	suffix, e := calendarSuffix(in.CalendarID)
	if e != nil {
		return nil, e
	}
	base, e := a.delegated(ctx)
	if e != nil {
		return nil, e
	}
	var values []rawEvent
	q := url.Values{"startDateTime": {s.Format(time.RFC3339Nano)}, "endDateTime": {f.Format(time.RFC3339Nano)}, "$top": {strconv.Itoa(n)}, "$select": {eventFields}}
	next, e := a.page(ctx, base+suffix+"/calendarView", q, in.NextLink, &values)
	if e != nil {
		return nil, e
	}
	out := &CalendarViewOut{Events: []CalendarEvent{}, NextLink: next}
	for _, v := range values {
		if v.ID == "" {
			return nil, fmt.Errorf("Microsoft Graph returned an event without its ID")
		}
		out.Events = append(out.Events, v.typed())
	}
	return out, nil
}
func (a *api) create_event(ctx context.Context, in *CreateEventIn) (*CreateEventOut, error) {
	if strings.TrimSpace(in.Subject) == "" {
		return nil, fmt.Errorf("subject is required")
	}
	s, f, e := timeRange(in.Start, in.End)
	if e != nil {
		return nil, e
	}
	kind, e := contentType(in.BodyType)
	if e != nil {
		return nil, e
	}
	suffix, e := calendarSuffix(in.CalendarID)
	if e != nil {
		return nil, e
	}
	if len(in.Attendees) > 100 {
		return nil, fmt.Errorf("at most 100 attendees are supported")
	}
	people, e := recipients(in.Attendees)
	if e != nil {
		return nil, e
	}
	attendees := []any{}
	for _, p := range people {
		attendees = append(attendees, map[string]any{"emailAddress": p.EmailAddress, "type": "required"})
	}
	payload := map[string]any{"subject": in.Subject, "start": dateTimeZone{s.Format("2006-01-02T15:04:05.999999999"), "UTC"}, "end": dateTimeZone{f.Format("2006-01-02T15:04:05.999999999"), "UTC"}, "body": itemBody{kind, in.Body}, "attendees": attendees}
	if in.Location != "" {
		payload["location"] = map[string]string{"displayName": in.Location}
	}
	if in.TransactionID != "" {
		if len(in.TransactionID) > 256 || strings.ContainsAny(in.TransactionID, "\r\n") {
			return nil, fmt.Errorf("transaction_id must be at most 256 characters without line breaks")
		}
		payload["transactionId"] = in.TransactionID
	}
	base, e := a.delegated(ctx)
	if e != nil {
		return nil, e
	}
	var r rawEvent
	if e = a.json(ctx, "POST", base+suffix+"/events", nil, payload, &r, 201); e != nil {
		return nil, e
	}
	if r.ID == "" {
		return nil, fmt.Errorf("Microsoft Graph did not confirm event creation; inspect the calendar before retrying")
	}
	return &CreateEventOut{Event: r.typed()}, nil
}
