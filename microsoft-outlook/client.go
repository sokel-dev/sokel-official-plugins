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

const graphOrigin = "https://graph.microsoft.com"
const jsonLimit = 16 << 20
const requestLimit = 4 << 20

type api struct {
	token  string
	client *http.Client
}

func newAPI(token string) *api {
	return &api{token: token, client: &http.Client{Timeout: 55 * time.Second}}
}

type apiError struct{ status int }

func (e *apiError) Error() string {
	switch e.status {
	case 400:
		return "Microsoft Graph rejected the request; check IDs, supported fields and input format"
	case 401:
		return "Microsoft Graph token is invalid or expired; refresh it externally and replace the credential"
	case 403:
		return "Microsoft Graph denied access; check delegated scopes, tenant consent and resource membership"
	case 404:
		return "Microsoft Graph resource was not found or is not accessible"
	case 409:
		return "Microsoft Graph reported a conflict; inspect the resource before retrying"
	case 429:
		return "Microsoft Graph throttled this request; wait before retrying"
	}
	return fmt.Sprintf("Microsoft Graph returned HTTP %d", e.status)
}
func segment(id, name string) (string, error) {
	if strings.TrimSpace(id) == "" || id == "." || id == ".." || len(id) > 2048 || strings.ContainsAny(id, "/\\?#%\r\n\x00") {
		return "", fmt.Errorf("%s must be a nonempty unescaped Graph resource ID without path separators", name)
	}
	return url.PathEscape(id), nil
}
func pageSize(n int) (int, error) {
	if n == 0 {
		return 50, nil
	}
	if n < 1 || n > 50 {
		return 0, fmt.Errorf("page size must be between 1 and 50")
	}
	return n, nil
}
func contentType(s string) (string, error) {
	s = strings.ToLower(s)
	if s == "" {
		return "text", nil
	}
	if s != "text" && s != "html" {
		return "", fmt.Errorf("content type must be text or html")
	}
	return s, nil
}
func (a *api) request(ctx context.Context, method, path string, q url.Values, body any, expected int) ([]byte, error) {
	if strings.TrimSpace(a.token) == "" || strings.ContainsAny(a.token, "\r\n") {
		return nil, fmt.Errorf("a valid delegated Microsoft Graph access token is required")
	}
	if !strings.HasPrefix(path, "/v1.0/") || strings.ContainsAny(path, "?#\r\n") {
		return nil, fmt.Errorf("invalid Microsoft Graph resource path")
	}
	endpoint := graphOrigin + path
	if len(q) > 0 {
		endpoint += "?" + q.Encode()
	}
	return a.send(ctx, method, endpoint, body, expected)
}
func (a *api) send(ctx context.Context, method, endpoint string, body any, expected int) ([]byte, error) {
	// Only request and page call this function, after origin/path validation.
	var raw []byte
	var e error
	if body != nil {
		raw, e = json.Marshal(body)
		if e != nil {
			return nil, fmt.Errorf("request contains invalid JSON values")
		}
		if len(raw) > requestLimit {
			return nil, fmt.Errorf("request exceeds 4 MiB")
		}
	}
	req, e := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(raw))
	if e != nil {
		return nil, fmt.Errorf("invalid Microsoft Graph request")
	}
	req.Header.Set("Authorization", "Bearer "+a.token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Prefer", `IdType="ImmutableId", outlook.timezone="UTC"`)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := *a.client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if client.Timeout == 0 {
		client.Timeout = 55 * time.Second
	}
	res, e := client.Do(req)
	if e != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("Microsoft Graph transport failed; check connectivity")
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, &apiError{res.StatusCode}
	}
	if res.StatusCode != expected {
		return nil, fmt.Errorf("Microsoft Graph returned an unexpected success status; inspect the resource before retrying")
	}
	b, e := io.ReadAll(io.LimitReader(res.Body, jsonLimit+1))
	if e != nil {
		return nil, fmt.Errorf("failed to read Microsoft Graph response")
	}
	if len(b) > jsonLimit {
		return nil, fmt.Errorf("Microsoft Graph response exceeds 16 MiB")
	}
	return b, nil
}
func (a *api) json(ctx context.Context, method, path string, q url.Values, input, out any, status int) error {
	raw, e := a.request(ctx, method, path, q, input, status)
	if e != nil {
		return e
	}
	if out != nil {
		if e = json.Unmarshal(raw, out); e != nil {
			return fmt.Errorf("Microsoft Graph returned malformed JSON")
		}
	}
	return nil
}
func (a *api) delegated(ctx context.Context) (string, error) {
	var r struct {
		ID string `json:"id"`
	}
	if e := a.json(ctx, "GET", "/v1.0/me", url.Values{"$select": {"id"}}, nil, &r, 200); e != nil {
		return "", e
	}
	id, e := segment(r.ID, "authenticated user ID")
	if e != nil {
		return "", fmt.Errorf("Microsoft Graph did not return an authenticated delegated user")
	}
	return "/v1.0/users/" + id, nil
}
func (a *api) health_check(ctx context.Context, _ *HealthCheckIn) (*HealthCheckOut, error) {
	_, e := a.delegated(ctx)
	if e != nil {
		return &HealthCheckOut{Message: e.Error()}, nil
	}
	return &HealthCheckOut{OK: true, Message: "Delegated identity verified; each operation also requires its service scopes and resource permissions"}, nil
}
func validateNext(path, next string) error {
	if next == "" {
		return nil
	}
	u, e := url.Parse(next)
	decoded, e2 := url.PathUnescape(path)
	if e != nil || e2 != nil || u.Scheme != "https" || u.Host != "graph.microsoft.com" || u.User != nil || u.Fragment != "" || u.Path != decoded || u.RawQuery == "" || len(next) > 32768 {
		return fmt.Errorf("pagination URL must remain on the same global Microsoft Graph resource")
	}
	return nil
}
func (a *api) page(ctx context.Context, path string, q url.Values, next string, out any) (string, error) {
	if e := validateNext(path, next); e != nil {
		return "", e
	}
	var raw []byte
	var e error
	if next == "" {
		raw, e = a.request(ctx, "GET", path, q, nil, 200)
	} else {
		raw, e = a.send(ctx, "GET", next, nil, 200)
	}
	if e != nil {
		return "", e
	}
	var r struct {
		Value json.RawMessage `json:"value"`
		Next  string          `json:"@odata.nextLink"`
	}
	if json.Unmarshal(raw, &r) != nil || len(r.Value) == 0 || string(r.Value) == "null" || r.Value[0] != '[' {
		return "", fmt.Errorf("Microsoft Graph returned an incomplete collection")
	}
	if json.Unmarshal(r.Value, out) != nil {
		return "", fmt.Errorf("Microsoft Graph returned an invalid collection shape")
	}
	if e = validateNext(path, r.Next); e != nil {
		return "", e
	}
	if r.Next != "" && r.Next == next {
		return "", fmt.Errorf("Microsoft Graph pagination made no progress")
	}
	return r.Next, nil
}
