package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const jsonLimit int64 = 8 << 20

type api struct {
	token  string
	client *http.Client
}

func newAPI(token string) *api {
	return &api{token: token, client: &http.Client{Timeout: 55 * time.Second}}
}

// Fixed service origin, no redirects and no automatic retries (including writes).
func (a *api) json(ctx context.Context, method, path string, q url.Values, body, out any) error {
	if strings.TrimSpace(a.token) == "" || strings.ContainsAny(a.token, "\r\n") {
		return fmt.Errorf("missing or invalid ClickUp credential")
	}
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || strings.ContainsAny(path, "?#\r\n") {
		return fmt.Errorf("invalid API path")
	}
	var raw []byte
	var err error
	if body != nil {
		raw, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("request contains invalid JSON")
		}
		if int64(len(raw)) > jsonLimit {
			return fmt.Errorf("JSON request exceeds 8 MiB")
		}
	}
	target := "https://api.clickup.com/api/v2" + path
	if len(q) > 0 {
		target += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("invalid ClickUp request")
	}
	req.Header.Set("Authorization", ""+a.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := *a.client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if client.Timeout == 0 {
		client.Timeout = 55 * time.Second
	}
	res, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("ClickUp transport failed; check connectivity")
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		switch res.StatusCode {
		case 401:
			return fmt.Errorf("ClickUp credential expired or invalid; replace the token")
		case 403:
			return fmt.Errorf("ClickUp denied access; check granted permissions and resource membership")
		case 429:
			return fmt.Errorf("ClickUp rate limit reached; retry later")
		}
		return fmt.Errorf("ClickUp API returned HTTP %d", res.StatusCode)
	}
	raw, err = io.ReadAll(io.LimitReader(res.Body, jsonLimit+1))
	if err != nil {
		return fmt.Errorf("failed to read ClickUp response")
	}
	if int64(len(raw)) > jsonLimit {
		return fmt.Errorf("ClickUp response exceeds 8 MiB")
	}
	if err = json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("ClickUp returned malformed JSON")
	}
	return nil
}
func validID(id string) error {
	if id == "" || len(id) > 256 {
		return fmt.Errorf("resource ID is required and must not exceed 256 characters")
	}
	for _, r := range id {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r == '_' || r == '-') {
			return fmt.Errorf("invalid resource ID")
		}
	}
	return nil
}
