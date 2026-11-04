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

const requestLimit = 4 << 20
const responseLimit = 8 << 20

type api struct {
	cred   Cred
	client *http.Client
}

func newAPI(c Cred) *api { return &api{cred: c, client: &http.Client{Timeout: 55 * time.Second}} }

type apiError struct{ Status int }

func (e *apiError) Error() string {
	switch e.Status {
	case 400, 422:
		return "Linear rejected request fields; check IDs, field configuration and input format"
	case 401:
		return "Linear authentication failed; check the credential and token expiration"
	case 403:
		return "Linear denied access; check token scopes and resource permissions"
	case 404:
		return "Linear resource was not found or is not accessible"
	case 409:
		return "Linear version conflict; fetch the latest resource and resolve changes before retrying"
	case 429:
		return "Linear rate limit reached; wait before retrying"
	}
	return fmt.Sprintf("Linear API returned HTTP %d", e.Status)
}
func (a *api) request(ctx context.Context, method, path string, q url.Values, input, out any) (http.Header, error) {
	base, err := a.base()
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || strings.ContainsAny(path, "?#\r\n") {
		return nil, fmt.Errorf("invalid API path")
	}
	var raw []byte
	if input != nil {
		raw, err = json.Marshal(input)
		if err != nil {
			return nil, fmt.Errorf("request contains invalid JSON values")
		}
		if len(raw) > requestLimit {
			return nil, fmt.Errorf("request exceeds 4 MiB")
		}
	}
	endpoint := base + path
	if len(q) > 0 {
		endpoint += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("invalid Linear request")
	}
	req.Header.Set("Authorization", a.cred.APIKey)
	req.Header.Set("Accept", "application/json")
	if input != nil {
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
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("Linear request transport failed; check connectivity")
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, &apiError{Status: res.StatusCode}
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, responseLimit+1))
	if err != nil {
		return nil, fmt.Errorf("failed to read Linear response")
	}
	if len(b) > responseLimit {
		return nil, fmt.Errorf("Linear response exceeds 8 MiB")
	}
	if out != nil {
		if err = json.Unmarshal(b, out); err != nil {
			return nil, fmt.Errorf("Linear returned malformed JSON")
		}
	}
	return res.Header, nil
}
func pageSize(n int) (int, error) {
	if n == 0 {
		return 50, nil
	}
	if n < 1 || n > 100 {
		return 0, fmt.Errorf("page size must be between 1 and 100")
	}
	return n, nil
}
func required(s, name string) error {
	if strings.TrimSpace(s) == "" || strings.ContainsAny(s, "\r\n") {
		return fmt.Errorf("%s is required and must not contain line breaks", name)
	}
	return nil
}
func validDate(s string) error {
	if s == "" {
		return nil
	}
	d, e := time.Parse("2006-01-02", s)
	if e != nil || d.Format("2006-01-02") != s {
		return fmt.Errorf("date must use YYYY-MM-DD")
	}
	return nil
}
func (a *api) base() (string, error) {
	if strings.TrimSpace(a.cred.APIKey) == "" || strings.ContainsAny(a.cred.APIKey, "\r\n") {
		return "", fmt.Errorf("missing or invalid Linear personal API key")
	}
	return "https://api.linear.app", nil
}
