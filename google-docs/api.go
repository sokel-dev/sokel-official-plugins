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

const jsonLimit int64 = 16 << 20

type api struct {
	token  string
	client *http.Client
}
type apiError struct{ Status int }

func (e *apiError) Error() string {
	switch e.Status {
	case 401:
		return "Google authorization expired; authorize this credential again"
	case 403:
		return "Google denied this request; check API enablement, granted scopes and resource access"
	case 404:
		return "Google resource was not found or is not accessible"
	case 400:
		return "Google rejected the request; check document indexes, tab IDs and required revision"
	case 409:
		return "Google reported an edit conflict; read the document again before applying the edit"
	case 429:
		return "Google quota exceeded; retry later"
	}
	return fmt.Sprintf("Google API returned HTTP %d", e.Status)
}
func newAPI(token string) *api {
	return &api{token: token, client: &http.Client{Timeout: 90 * time.Second}}
}

// Origins are fixed in code. Requests never follow redirects or include upstream response bodies in errors.
func (a *api) request(ctx context.Context, method, origin, path string, q url.Values, body []byte, mimeType string, limit int64) ([]byte, http.Header, error) {
	if strings.TrimSpace(a.token) == "" {
		return nil, nil, fmt.Errorf("missing Google access token; authorize the credential")
	}
	if origin != "https://www.googleapis.com" && origin != "https://docs.googleapis.com" {
		return nil, nil, fmt.Errorf("unsupported API origin")
	}
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || strings.ContainsAny(path, "?#\r\n") {
		return nil, nil, fmt.Errorf("invalid API path")
	}
	u := origin + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, bytes.NewReader(body))
	if err != nil {
		return nil, nil, fmt.Errorf("invalid Google request")
	}
	req.Header.Set("Authorization", "Bearer "+a.token)
	if mimeType != "" {
		req.Header.Set("Content-Type", mimeType)
	}
	req.Header.Set("Accept", "application/json")
	client := *a.client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if client.Timeout == 0 {
		client.Timeout = 90 * time.Second
	}
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		return nil, nil, fmt.Errorf("Google request transport failed; check connectivity")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, nil, &apiError{Status: resp.StatusCode}
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read Google response")
	}
	if int64(len(raw)) > limit {
		return nil, nil, fmt.Errorf("Google response exceeds the %d byte limit", limit)
	}
	return raw, resp.Header, nil
}
func (a *api) json(ctx context.Context, method, origin, path string, q url.Values, body any, out any) error {
	var raw []byte
	var err error
	if body != nil {
		raw, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("request contains invalid JSON values")
		}
		if int64(len(raw)) > jsonLimit {
			return fmt.Errorf("JSON request exceeds 16 MiB")
		}
	}
	raw, _, err = a.request(ctx, method, origin, path, q, raw, "application/json", jsonLimit)
	if err != nil {
		return err
	}
	if out != nil {
		if err = json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("Google returned malformed JSON")
		}
	}
	return nil
}
func (a *api) health(ctx context.Context) (*HealthCheckOut, error) {
	var user struct {
		Email string `json:"email"`
	}
	err := a.json(ctx, "GET", "https://www.googleapis.com", "/oauth2/v3/userinfo", nil, nil, &user)
	if err != nil {
		return &HealthCheckOut{Message: err.Error()}, nil
	}
	if user.Email == "" {
		return &HealthCheckOut{Message: "Google did not return an account email; check userinfo.email scope"}, nil
	}
	return &HealthCheckOut{OK: true, Message: "Google authorization is valid for " + user.Email + "; service and resource permissions are checked by each operation"}, nil
}
