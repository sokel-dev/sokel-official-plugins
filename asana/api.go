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
		return fmt.Errorf("missing or invalid Asana credential")
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
	target := "https://app.asana.com/api/1.0" + path
	if len(q) > 0 {
		target += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("invalid Asana request")
	}
	req.Header.Set("Authorization", "Bearer "+a.token)
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
		return fmt.Errorf("Asana transport failed; check connectivity")
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		switch res.StatusCode {
		case 401:
			return fmt.Errorf("Asana credential expired or invalid; replace the token")
		case 403:
			return fmt.Errorf("Asana denied access; check granted permissions and resource membership")
		case 429:
			return fmt.Errorf("Asana rate limit reached; retry later")
		}
		return fmt.Errorf("Asana API returned HTTP %d", res.StatusCode)
	}
	raw, err = io.ReadAll(io.LimitReader(res.Body, jsonLimit+1))
	if err != nil {
		return fmt.Errorf("failed to read Asana response")
	}
	if int64(len(raw)) > jsonLimit {
		return fmt.Errorf("Asana response exceeds 8 MiB")
	}
	if err = json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("Asana returned malformed JSON")
	}
	return nil
}
func validID(id string) error {
	if id == "" || len(id) > 256 {
		return fmt.Errorf("resource ID is required and must not exceed 256 characters")
	}
	for _, r := range id {
		if !(r >= '0' && r <= '9') {
			return fmt.Errorf("invalid resource ID")
		}
	}
	return nil
}
