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
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const maxResponse = 4 << 20
const maxFile = 20 << 20

type client struct {
	base, token, uploadOrigin string
	http                      *http.Client
}
type rateLimitError struct{ after time.Duration }

func (e *rateLimitError) Error() string {
	return fmt.Sprintf("Slack rate limit reached; retry after %d seconds. The request was not retried automatically.", int(e.after.Seconds()))
}
func newClient(token string) *client {
	base := strings.TrimRight(os.Getenv("SLACK_API_BASE_URL"), "/")
	if base == "" {
		base = "https://slack.com/api"
	}
	return &client{base: base, token: strings.TrimSpace(token), uploadOrigin: "https://files.slack.com", http: &http.Client{Timeout: 40 * time.Second}}
}
func (c *client) httpClient() *http.Client {
	h := http.Client{Timeout: 40 * time.Second}
	if c.http != nil {
		h = *c.http
	}
	h.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &h
}
func validBase(raw string) bool {
	u, e := url.Parse(raw)
	return e == nil && u.Host != "" && (u.Scheme == "https" || u.Scheme == "http") && u.User == nil && u.RawQuery == "" && u.Fragment == ""
}
func safeNetworkError(ctx context.Context) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return errors.New("Cannot reach Slack. Check the plugin server network and deployment configuration.")
}
func retryDelay(value string) time.Duration {
	if n, e := strconv.Atoi(value); e == nil && n > 0 && n <= 86400 {
		return time.Duration(n) * time.Second
	}
	if d, e := http.ParseTime(value); e == nil {
		v := time.Until(d)
		if v > 0 && v <= 24*time.Hour {
			return v.Round(time.Second) + time.Second
		}
	}
	return time.Minute
}
func (c *client) request(ctx context.Context, method, path string, q url.Values, body any, out any) error {
	if c.token == "" {
		return errors.New("Missing Slack access token. Configure token in the plugin credential.")
	}
	if !validBase(c.base) {
		return errors.New("Invalid SLACK_API_BASE_URL deployment configuration.")
	}
	var data []byte
	var err error
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			return errors.New("Request content cannot be encoded as JSON.")
		}
	}
	endpoint := c.base + "/" + path
	if len(q) > 0 {
		endpoint += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(data))
	if err != nil {
		return errors.New("Invalid Slack request configuration.")
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
	}
	res, err := c.httpClient().Do(req)
	if err != nil {
		return safeNetworkError(ctx)
	}
	defer res.Body.Close()
	if res.StatusCode == 429 {
		return &rateLimitError{retryDelay(res.Header.Get("Retry-After"))}
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("Slack HTTP %d. Check permissions, service status and deployment endpoint. Writes are not retried automatically.", res.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, maxResponse+1))
	if err != nil {
		return errors.New("Failed to read the Slack response.")
	}
	if len(b) > maxResponse {
		return errors.New("Slack response exceeds 4 MiB. Narrow the query.")
	}
	var envelope struct {
		OK    *bool  `json:"ok"`
		Error string `json:"error"`
	}
	if json.Unmarshal(b, &envelope) != nil || envelope.OK == nil {
		return errors.New("Slack returned an invalid JSON response.")
	}
	if !*envelope.OK {
		return slackError(envelope.Error)
	}
	if json.Unmarshal(b, out) != nil {
		return errors.New("Slack response does not match the expected structure.")
	}
	return nil
}
func slackError(code string) error {
	messages := map[string]string{"invalid_auth": "Invalid access token", "not_authed": "Missing access token", "token_revoked": "Access token has been revoked", "account_inactive": "Account is inactive", "missing_scope": "Access token lacks the required scopes", "not_in_channel": "Invite the bot to the target channel first", "channel_not_found": "Channel does not exist or the token cannot access it", "thread_not_found": "Thread does not exist or does not support replies", "is_archived": "Channel is archived", "restricted_action": "Workspace policy prohibits this operation", "file_not_found": "File does not exist", "no_permission": "Permission denied for this operation", "not_allowed_token_type": "This operation does not support the current token type", "ratelimited": "Rate limited; wait at least 60 seconds before retrying"}
	if msg, ok := messages[code]; ok {
		return fmt.Errorf("Slack: %s (%s)", msg, code)
	}
	return errors.New("Slack rejected the request. Check parameters, application permissions and Slack service status.")
}

var channelPattern = regexp.MustCompile(`^[CDG][A-Z0-9]{2,64}$`)
var tsPattern = regexp.MustCompile(`^[0-9]{1,16}\.[0-9]{6}$`)

func validChannel(s string) error {
	if !channelPattern.MatchString(s) {
		return errors.New("Invalid channel ID. Enter a Slack ID starting with C, D or G.")
	}
	return nil
}
func validTS(s string, required bool) error {
	if s == "" && !required {
		return nil
	}
	if !tsPattern.MatchString(s) {
		return errors.New("Invalid message timestamp. Preserve the complete ts string returned by Slack.")
	}
	return nil
}
func (c *client) health(ctx context.Context) (*HealthCheckOut, error) {
	var out HealthCheckOut
	if err := c.request(ctx, "POST", "auth.test", nil, struct{}{}, &out); err != nil {
		return nil, err
	}
	if out.TeamID == "" || out.UserID == "" {
		return nil, errors.New("Slack identity response is missing the workspace or user ID.")
	}
	return &out, nil
}

type historyResponse struct {
	Messages []Message `json:"messages"`
	HasMore  bool      `json:"has_more"`
	Metadata struct {
		Next string `json:"next_cursor"`
	} `json:"response_metadata"`
}

func (c *client) page(ctx context.Context, method string, in *SlackHistoryIn, ts string) (*SlackHistoryOut, error) {
	if err := validChannel(in.Channel); err != nil {
		return nil, err
	}
	for _, v := range []string{in.Oldest, in.Latest} {
		if err := validTS(v, false); err != nil {
			return nil, err
		}
	}
	if len(in.Cursor) > 4096 {
		return nil, errors.New("Pagination cursor is too long.")
	}
	q := url.Values{"channel": {in.Channel}, "limit": {"15"}}
	for k, v := range map[string]string{"cursor": in.Cursor, "oldest": in.Oldest, "latest": in.Latest, "ts": ts} {
		if v != "" {
			q.Set(k, v)
		}
	}
	var r historyResponse
	if err := c.request(ctx, "GET", method, q, nil, &r); err != nil {
		return nil, err
	}
	if r.Messages == nil {
		return nil, errors.New("Slack response is missing the messages array.")
	}
	for _, m := range r.Messages {
		if err := validTS(m.Ts, true); err != nil {
			return nil, errors.New("Slack message response is missing a valid timestamp.")
		}
	}
	return &SlackHistoryOut{Messages: r.Messages, HasMore: r.HasMore, NextCursor: r.Metadata.Next}, nil
}
func (c *client) history(ctx context.Context, in *SlackHistoryIn) (*SlackHistoryOut, error) {
	return c.page(ctx, "conversations.history", in, "")
}
func (c *client) replies(ctx context.Context, in *SlackRepliesIn) (*SlackRepliesOut, error) {
	if err := validTS(in.Ts, true); err != nil {
		return nil, err
	}
	r, e := c.page(ctx, "conversations.replies", &SlackHistoryIn{Channel: in.Channel, Cursor: in.Cursor, Oldest: in.Oldest, Latest: in.Latest}, in.Ts)
	if e != nil {
		return nil, e
	}
	return &SlackRepliesOut{Messages: r.Messages, NextCursor: r.NextCursor, HasMore: r.HasMore}, nil
}
func (c *client) send(ctx context.Context, in *SlackSendIn) (*SlackSendOut, error) {
	if err := validChannel(in.Channel); err != nil {
		return nil, err
	}
	if err := validTS(in.ThreadTs, false); err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.Text) == "" || utf8.RuneCountInString(in.Text) > 40000 {
		return nil, errors.New("Message text must contain 1 to 40000 characters.")
	}
	body := map[string]any{"channel": in.Channel, "text": in.Text}
	if in.ThreadTs != "" {
		body["thread_ts"] = in.ThreadTs
	}
	var out SlackSendOut
	if e := c.request(ctx, "POST", "chat.postMessage", nil, body, &out); e != nil {
		return nil, e
	}
	if validTS(out.Ts, true) != nil || out.Channel == "" {
		return nil, errors.New("Slack responded without a valid message receipt. Check the channel before sending again.")
	}
	return &out, nil
}
func (c *client) upload(ctx context.Context, in *SlackUploadIn, data []byte) (*SlackUploadOut, error) {
	if err := validateUpload(in); err != nil {
		return nil, err
	}
	if len(data) == 0 || len(data) > maxFile {
		return nil, errors.New("File size must be between 1 byte and 20 MiB.")
	}
	var lease struct {
		URL string `json:"upload_url"`
		ID  string `json:"file_id"`
	}
	if err := c.request(ctx, "POST", "files.getUploadURLExternal", nil, map[string]any{"filename": in.File.Name, "length": len(data)}, &lease); err != nil {
		return nil, err
	}
	u, e := url.Parse(lease.URL)
	origin := c.uploadOrigin
	if origin == "" {
		origin = "https://files.slack.com"
	}
	allowed, e2 := url.Parse(origin)
	if e != nil || e2 != nil || u.User != nil || u.Fragment != "" || u.Scheme != allowed.Scheme || u.Host != allowed.Host || !strings.HasPrefix(u.Path, "/upload/") || lease.ID == "" {
		return nil, errors.New("Slack returned an untrusted file upload URL.")
	}
	req, e := http.NewRequestWithContext(ctx, "POST", lease.URL, bytes.NewReader(data))
	if e != nil {
		return nil, errors.New("Invalid file upload URL.")
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	res, e := c.httpClient().Do(req)
	if e != nil {
		return nil, safeNetworkError(ctx)
	}
	defer res.Body.Close()
	if res.StatusCode == 429 {
		return nil, &rateLimitError{retryDelay(res.Header.Get("Retry-After"))}
	}
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("Slack file upload returned HTTP %d. The upload was not retried automatically.", res.StatusCode)
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
	file := map[string]string{"id": lease.ID}
	if in.Title != "" {
		file["title"] = in.Title
	}
	body := map[string]any{"files": []map[string]string{file}, "channel_id": in.Channel}
	if in.InitialComment != "" {
		body["initial_comment"] = in.InitialComment
	}
	if in.ThreadTs != "" {
		body["thread_ts"] = in.ThreadTs
	}
	var out struct {
		Files []struct {
			ID string `json:"id"`
		} `json:"files"`
	}
	if err := c.request(ctx, "POST", "files.completeUploadExternal", nil, body, &out); err != nil {
		return nil, err
	}
	for _, f := range out.Files {
		if f.ID == lease.ID {
			return &SlackUploadOut{FileID: lease.ID}, nil
		}
	}
	return nil, errors.New("Slack upload completion response is missing a valid receipt. Check the file before uploading again.")
}
func validateUpload(in *SlackUploadIn) error {
	if err := validChannel(in.Channel); err != nil {
		return err
	}
	if err := validTS(in.ThreadTs, false); err != nil {
		return err
	}
	if in.File == nil || strings.TrimSpace(in.File.Name) == "" || strings.ContainsAny(in.File.Name, "\r\n\x00") || len(in.File.Name) > 255 {
		return errors.New("Provide a platform file with a valid filename.")
	}
	if in.File.Size > maxFile || in.File.Size < 0 {
		return errors.New("File must not exceed 20 MiB.")
	}
	if utf8.RuneCountInString(in.InitialComment) > 40000 {
		return errors.New("Initial comment must not exceed 40000 characters.")
	}
	return nil
}
