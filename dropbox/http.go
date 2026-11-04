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
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const maxJSON = 8 << 20
const maxFile = 32 << 20
const maxRequest = 1 << 20

type client struct {
	base, contentBase string
	cred              Cred
	http              *http.Client
}

func newClient(cred Cred) *client {
	base := strings.TrimRight(os.Getenv("SOKEL_DROPBOX_BASE_URL"), "/")
	if base == "" {
		base = "https://api.dropboxapi.com"
	}
	content := strings.TrimRight(os.Getenv("SOKEL_DROPBOX_CONTENT_BASE_URL"), "/")
	if content == "" {
		content = "https://content.dropboxapi.com"
	}
	return &client{base: base, contentBase: content, cred: cred}
}
func validBase(raw string) bool {
	u, e := url.Parse(raw)
	if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	if u.Scheme == "https" {
		return true
	}
	ip := net.ParseIP(u.Hostname())
	return u.Scheme == "http" && (u.Hostname() == "localhost" || (ip != nil && ip.IsLoopback()))
}
func bounded(r io.Reader, limit int) ([]byte, error) {
	b, e := io.ReadAll(io.LimitReader(r, int64(limit)+1))
	if e != nil {
		return nil, errors.New("Could not read the upstream response.")
	}
	if len(b) > limit {
		return nil, errors.New("Upstream response exceeds the operation size limit.")
	}
	return b, nil
}
func retryDelay(s string) int {
	if n, e := strconv.Atoi(s); e == nil && n > 0 && n <= 86400 {
		return n
	}
	return 60
}
func apiError(status int, h http.Header) error {
	switch status {
	case 401:
		return errors.New("Dropbox rejected the access token. Supply a current token; automatic refresh is not implemented.")
	case 403:
		return errors.New("Dropbox denied access. Check app scopes and item permissions.")
	case 404:
		return errors.New("Dropbox item was not found or is inaccessible.")
	case 409:
		return errors.New("Dropbox reported a path, cursor or item conflict. Check the item; restart a listing if its cursor expired.")
	case 429:
		return fmt.Errorf("Dropbox rate limit reached; wait at least %d seconds. No automatic retry was made.", retryDelay(h.Get("Retry-After")))
	}
	return fmt.Errorf("Dropbox returned HTTP %d. No automatic retry was made.", status)
}
func (c *client) do(ctx context.Context, method, endpoint string, body []byte, headers http.Header, auth bool, timeout time.Duration) (*http.Response, error) {
	if len(body) > maxFile+(1<<20) {
		return nil, errors.New("Request exceeds the operation size limit.")
	}
	req, e := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if e != nil {
		return nil, errors.New("Invalid Dropbox request configuration.")
	}
	req.Header = headers.Clone()
	if req.Header == nil {
		req.Header = make(http.Header)
	}
	if auth {
		token := strings.TrimSpace(c.cred.AccessToken)
		if token == "" || strings.ContainsAny(token, "\r\n") {
			return nil, errors.New("Configure a valid Dropbox access token.")
		}
		req.Header.Set("Authorization", "Bearer "+token)
	}
	h := http.Client{Timeout: timeout}
	if c.http != nil {
		h = *c.http
		if h.Timeout == 0 {
			h.Timeout = timeout
		}
	}
	h.Jar = nil
	h.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, e := h.Do(req)
	if e != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("Cannot reach Dropbox or the request timed out. Check deployment networking; no automatic retry was made.")
	}
	return res, nil
}
func (c *client) endpoint(base, path string, q url.Values) (string, error) {
	if !validBase(base) {
		return "", errors.New("Use a trusted HTTPS deployment endpoint; HTTP is limited to loopback tests.")
	}
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || strings.ContainsAny(path, "?#\r\n") {
		return "", errors.New("Invalid API request path.")
	}
	endpoint := base + path
	if len(q) > 0 {
		endpoint += "?" + q.Encode()
	}
	return endpoint, nil
}
func decode(b []byte, out any) error {
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	if d.Decode(out) != nil {
		return errors.New("Dropbox returned malformed JSON or an unexpected response shape.")
	}
	var more any
	if d.Decode(&more) != io.EOF {
		return errors.New("Dropbox returned extra data after JSON.")
	}
	return nil
}
func jsonResponse(res *http.Response, out any) error {
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return apiError(res.StatusCode, res.Header)
	}
	ct, _, e := mime.ParseMediaType(res.Header.Get("Content-Type"))
	if e != nil || ct != "application/json" {
		return errors.New("Dropbox did not return JSON.")
	}
	b, e := bounded(res.Body, maxJSON)
	if e != nil {
		return e
	}
	return decode(b, out)
}
func (c *client) json(ctx context.Context, method, path string, q url.Values, body any, out any) error {
	endpoint, e := c.endpoint(c.base, path, q)
	if e != nil {
		return e
	}
	var b []byte
	if body != nil {
		b, e = json.Marshal(body)
		if e != nil {
			return errors.New("Request contains invalid JSON values.")
		}
		if len(b) > maxRequest {
			return errors.New("JSON request exceeds 1 MiB.")
		}
	}
	res, e := c.do(ctx, method, endpoint, b, http.Header{"Content-Type": {"application/json"}, "Accept": {"application/json"}}, true, 45*time.Second)
	if e != nil {
		return e
	}
	return jsonResponse(res, out)
}
func validName(s string) bool {
	if s == "" || s == "." || s == ".." || len(s) > 255 || !utf8.ValidString(s) || strings.ContainsAny(s, "/\\") {
		return false
	}
	return strings.IndexFunc(s, unicode.IsControl) < 0
}
func fileMIME(s string) string {
	v, _, e := mime.ParseMediaType(s)
	if e != nil || !strings.Contains(v, "/") {
		return "application/octet-stream"
	}
	return v
}
