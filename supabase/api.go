package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const jsonLimit int64 = 16 << 20

type api struct {
	base, key, token string
	client           *http.Client
}

func newAPI(base, key, token string) *api {
	return &api{base: base, key: key, token: token, client: &http.Client{Timeout: 60 * time.Second}}
}
func origin(base string) (string, error) {
	u, e := url.Parse(strings.TrimSpace(base))
	if e != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", fmt.Errorf("SUPABASE_URL must be a project origin without a path, query or credentials")
	}
	ip := net.ParseIP(u.Hostname())
	loopback := u.Hostname() == "localhost" || (ip != nil && ip.IsLoopback())
	if u.Scheme != "https" && !(u.Scheme == "http" && loopback) {
		return "", fmt.Errorf("SUPABASE_URL requires HTTPS; HTTP is allowed only for local development")
	}
	return strings.TrimSuffix(u.String(), "/"), nil
}

type apiError struct{ status int }

func (e *apiError) Error() string {
	switch e.status {
	case 401:
		return "Supabase rejected the credential; check the project API key and user access token"
	case 403:
		return "Supabase denied access; check database grants, RLS policies and storage permissions"
	case 404:
		return "Supabase resource was not found; check the project, exposed schema and resource name"
	case 409:
		return "Supabase reported a data conflict; check unique constraints"
	case 429:
		return "Supabase rate limit exceeded; retry later"
	}
	return fmt.Sprintf("Supabase returned HTTP %d; check the operation parameters and project configuration", e.status)
}
func (a *api) request(ctx context.Context, method, path, schema string, q url.Values, body any, prefer string) ([]byte, http.Header, error) {
	base, e := origin(a.base)
	if e != nil {
		return nil, nil, e
	}
	if strings.TrimSpace(a.key) == "" {
		return nil, nil, fmt.Errorf("project API key is required")
	}
	if !strings.HasPrefix(path, "/rest/v1/") && !strings.HasPrefix(path, "/storage/v1/") {
		return nil, nil, fmt.Errorf("unsupported Supabase API path")
	}
	if strings.ContainsAny(path, "?#\r\n") {
		return nil, nil, fmt.Errorf("invalid Supabase API path")
	}
	if schema != "" && !identifier.MatchString(schema) {
		return nil, nil, fmt.Errorf("schema must be a simple exposed schema name")
	}
	var raw []byte
	if body != nil {
		raw, e = json.Marshal(body)
		if e != nil {
			return nil, nil, fmt.Errorf("request contains invalid JSON values")
		}
		if int64(len(raw)) > jsonLimit {
			return nil, nil, fmt.Errorf("request exceeds 16 MiB")
		}
	}
	u := base + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, e := http.NewRequestWithContext(ctx, method, u, bytes.NewReader(raw))
	if e != nil {
		return nil, nil, fmt.Errorf("invalid Supabase request")
	}
	req.Header.Set("apikey", a.key)
	token := a.token
	if token == "" && !strings.HasPrefix(a.key, "sb_publishable_") && !strings.HasPrefix(a.key, "sb_secret_") {
		token = a.key
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if schema != "" {
		if method == "GET" || method == "HEAD" {
			req.Header.Set("Accept-Profile", schema)
		} else {
			req.Header.Set("Content-Profile", schema)
		}
	}
	if prefer != "" {
		req.Header.Set("Prefer", prefer)
	}
	client := *a.client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if client.Timeout == 0 {
		client.Timeout = 60 * time.Second
	}
	resp, e := client.Do(req)
	if e != nil {
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		return nil, nil, fmt.Errorf("Supabase request transport failed; check connectivity")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, nil, &apiError{status: resp.StatusCode}
	}
	raw, e = io.ReadAll(io.LimitReader(resp.Body, jsonLimit+1))
	if e != nil {
		return nil, nil, fmt.Errorf("failed to read Supabase response")
	}
	if int64(len(raw)) > jsonLimit {
		return nil, nil, fmt.Errorf("Supabase response exceeds 16 MiB")
	}
	return raw, resp.Header, nil
}
func decode(raw []byte, out any) error {
	if !json.Valid(raw) {
		return fmt.Errorf("Supabase returned malformed JSON")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if e := decoder.Decode(out); e != nil {
		return fmt.Errorf("Supabase returned malformed or unexpected JSON")
	}
	return nil
}
