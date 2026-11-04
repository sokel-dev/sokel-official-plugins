package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

const maxResponseBytes = 16 << 20
const maxRequestBytes = 1 << 20

// Each plugin owns its small transport so it remains an independent public-SDK module.
// Base URLs are deployment configuration, never operation inputs or credential fields.
type apiClient struct {
	base string
	key  string
	http *http.Client
}

func newAPIClient(env, fallback, key string, required bool, timeout time.Duration) (*apiClient, error) {
	key = strings.TrimSpace(key)
	if required && key == "" {
		return nil, errors.New("Missing API key; configure it in the plugin credentials")
	}
	if strings.ContainsAny(key, "\r\n") {
		return nil, errors.New("Invalid API key format")
	}
	base := strings.TrimSpace(os.Getenv(env))
	if base == "" {
		base = fallback
	}
	u, e := url.Parse(base)
	if e != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return nil, errors.New("Invalid deployment base URL: use an HTTPS URL without credentials, query or fragment")
	}
	loopback := u.Hostname() == "localhost"
	if ip := net.ParseIP(u.Hostname()); ip != nil {
		loopback = ip.IsLoopback()
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && loopback) {
		return nil, errors.New("Deployment base URL must use HTTPS (HTTP is allowed only on loopback)")
	}
	return &apiClient{base: strings.TrimRight(u.String(), "/"), key: key, http: &http.Client{Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func (c *apiClient) request(ctx context.Context, method, path string, body any, headers map[string]string, out any) error {
	var payload []byte
	if body != nil {
		var e error
		payload, e = json.Marshal(body)
		if e != nil {
			return errors.New("Cannot encode request")
		}
		if len(payload) > maxRequestBytes {
			return errors.New("Request exceeds the 1 MiB limit")
		}
	}
	r, e := http.NewRequestWithContext(ctx, method, c.base+path, bytes.NewReader(payload))
	if e != nil {
		return errors.New("Cannot construct upstream request")
	}
	r.Header.Set("Accept", "application/json")
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	if c.key != "" {
		r.Header.Set("Authorization", "Bearer "+c.key)
	}
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	resp, e := c.http.Do(r)
	if e != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("Upstream request canceled or timed out: %w", ctx.Err())
		}
		return errors.New("Upstream connection failed or timed out; check deployment network settings")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		switch resp.StatusCode {
		case 401, 403:
			return fmt.Errorf("Upstream authentication or access rejected (HTTP %d); check the API key and permissions", resp.StatusCode)
		case 402:
			return errors.New("Upstream credits exhausted (HTTP 402)")
		case 429:
			return errors.New("Upstream rate limit reached (HTTP 429); retry later")
		}
		return fmt.Errorf("Upstream returned HTTP %d; redirects are not followed", resp.StatusCode)
	}
	typ, _, e := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if e != nil || !(typ == "application/json" || strings.HasSuffix(typ, "+json")) {
		return errors.New("Upstream returned a non-JSON response")
	}
	b, e := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if e != nil {
		return errors.New("Failed to read upstream response")
	}
	if len(b) > maxResponseBytes {
		return errors.New("Upstream response exceeds the 16 MiB limit; request fewer pages")
	}
	if e = json.Unmarshal(b, out); e != nil {
		return errors.New("Upstream returned malformed JSON or an unexpected response shape")
	}
	return nil
}
func pageURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if len(raw) > 8192 {
		return "", errors.New("URL exceeds 8192 bytes")
	}
	u, e := url.Parse(raw)
	if e != nil || u.Hostname() == "" || u.User != nil || u.Opaque != "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", errors.New("Provide an absolute HTTP or HTTPS URL without embedded credentials")
	}
	u.Fragment = ""
	return u.String(), nil
}

var idPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func validID(id string) error {
	if !idPattern.MatchString(id) {
		return errors.New("Invalid job ID; use the identifier returned by the submit operation")
	}
	return nil
}
