package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const maxResponse = 4 << 20
const maxRequest = 1 << 20

type client struct {
	http    *http.Client
	key     string
	origin  string
	initErr error
}
type apiError struct {
	Status     int
	RetryAfter time.Duration
}

func (e *apiError) Error() string {
	switch e.Status {
	case 401:
		return "Mailchimp rejected the API key; check the credential"
	case 403:
		return "Mailchimp denied access; check account permissions and IP restrictions"
	case 404:
		return "Mailchimp resource was not found or is not accessible"
	case 429:
		if e.RetryAfter > 0 {
			return fmt.Sprintf("Mailchimp rate limit reached; retry after %d seconds", int64(e.RetryAfter/time.Second))
		}
		return "Mailchimp rate limit reached; wait before retrying"
	}
	return fmt.Sprintf("Mailchimp request failed (HTTP %d); inspect the provider console", e.Status)
}
func newClient(cred *Cred) *client {
	c := &client{http: &http.Client{Timeout: 45 * time.Second, Transport: http.DefaultTransport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	if cred == nil || len(cred.APIKey) == 0 || len(cred.APIKey) > 512 || strings.TrimSpace(cred.APIKey) != cred.APIKey || strings.ContainsAny(cred.APIKey, "\r\n\t\x00") {
		c.initErr = fmt.Errorf("set a valid Mailchimp API key in the credential")
		return c
	}
	c.key = cred.APIKey
	parts := strings.Split(c.key, "-")
	dc := parts[len(parts)-1]
	if len(parts) < 2 || !regexp.MustCompile(`^us[1-9][0-9]{0,3}$`).MatchString(dc) {
		c.initErr = fmt.Errorf("Mailchimp API key must include a valid data-center suffix such as -us6")
		return c
	}
	c.origin = "https://" + dc + ".api.mailchimp.com"
	return c
}
func (c *client) request(ctx context.Context, method, path string, q url.Values, body any, expected int) ([]byte, error) {
	if c.initErr != nil {
		return nil, c.initErr
	}
	if !strings.HasPrefix(path, "/3.0/") || strings.ContainsAny(path, "?#\\\r\n") {
		return nil, fmt.Errorf("invalid API path")
	}
	var data []byte
	var err error
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("request body is not valid JSON")
		}
		if len(data) > maxRequest {
			return nil, fmt.Errorf("request body exceeds 1 MiB")
		}
	}
	u := c.origin + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("cannot construct Mailchimp request")
	}
	req.GetBody = nil
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.SetBasicAuth("sokel", c.key)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Mailchimp connection failed or timed out; a write may already have been accepted")
	}
	defer resp.Body.Close()
	if resp.StatusCode != expected {
		delay, _ := strconv.ParseInt(resp.Header.Get("Retry-After"), 10, 32)
		if delay < 0 || delay > 86400 {
			delay = 0
		}
		return nil, &apiError{Status: resp.StatusCode, RetryAfter: time.Duration(delay) * time.Second}
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil {
		return nil, fmt.Errorf("cannot read Mailchimp response; a write may already have been accepted")
	}
	if len(raw) > maxResponse {
		return nil, fmt.Errorf("Mailchimp response exceeds 4 MiB; reduce the page size")
	}
	if expected == 204 {
		if len(bytes.TrimSpace(raw)) != 0 {
			return nil, fmt.Errorf("unexpected body in Mailchimp empty response")
		}
		return raw, nil
	}
	var obj map[string]json.RawMessage
	if decode(raw, &obj) != nil || obj == nil {
		return nil, fmt.Errorf("Mailchimp returned an invalid JSON object")
	}
	if status, ok := obj["status"]; ok {
		var n int
		if json.Unmarshal(status, &n) == nil && n >= 400 {
			return nil, fmt.Errorf("Mailchimp returned an API error inside a success response")
		}
	}
	return raw, nil
}
func decode(raw []byte, out any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if err := d.Decode(out); err != nil {
		return fmt.Errorf("invalid response data")
	}
	var trailing any
	if d.Decode(&trailing) != io.EOF {
		return fmt.Errorf("unexpected trailing JSON")
	}
	return nil
}
func email(v string) error {
	if v == "" || len(v) > 254 || strings.TrimSpace(v) != v || strings.ContainsAny(v, "\r\n\x00") {
		return fmt.Errorf("a plain email address is required")
	}
	a, e := mail.ParseAddress(v)
	if e != nil || a.Address != v || a.Name != "" || !strings.Contains(v, "@") {
		return fmt.Errorf("a plain email address is required")
	}
	return nil
}
func timestamp(v string) error {
	if v == "" {
		return nil
	}
	if _, e := time.Parse(time.RFC3339Nano, v); e != nil {
		return fmt.Errorf("timestamp must include a timezone in RFC 3339 format")
	}
	return nil
}
func pagination(limit, offset *int64, max int) (int, int, error) {
	n, o := int64(50), int64(0)
	if limit != nil {
		n = *limit
	}
	if offset != nil {
		o = *offset
	}
	if n < 1 || n > int64(max) || o < 0 || o > 10000000 {
		return 0, 0, fmt.Errorf("page size must be 1..%d and offset 0..10000000", max)
	}
	return int(n), int(o), nil
}
func pageResult(total *int, n, offset, limit int) (bool, int, error) {
	if total == nil || *total < 0 || *total > 1000000000 || n > limit || n > *total || n > 0 && offset+n > *total || n == 0 && offset < *total {
		return false, 0, fmt.Errorf("Mailchimp returned inconsistent pagination; retry a fresh read")
	}
	if offset+n < *total {
		next := offset + n
		if next > 10000000 {
			return false, 0, fmt.Errorf("pagination exceeds the plugin offset limit")
		}
		return true, next, nil
	}
	return false, 0, nil
}

var positiveDigits = regexp.MustCompile(`^[1-9][0-9]*$`)

func numericID(v string) error {
	if !positiveDigits.MatchString(v) {
		return fmt.Errorf("ID must be a positive decimal integer")
	}
	if _, e := strconv.ParseInt(v, 10, 64); e != nil {
		return fmt.Errorf("ID is outside the supported integer range")
	}
	return nil
}

type exactID string

func (v *exactID) UnmarshalJSON(raw []byte) error {
	s := string(raw)
	if numericID(s) != nil {
		return fmt.Errorf("invalid numeric ID")
	}
	*v = exactID(s)
	return nil
}
