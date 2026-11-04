package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const maxJSON int64 = 8 << 20
const maxFile int64 = 32 << 20
const requestTimeout = 240 * time.Second

type client struct {
	base string
	cred Cred
	http *http.Client
}

func newClient(base string, cred Cred) *client {
	return &client{base: base, cred: cred, http: &http.Client{Timeout: requestTimeout}}
}
func (c *client) endpoint(route string, q url.Values) (*url.URL, error) {
	if c.base == "" {
		return nil, errors.New("Configure SOKEL_PAPERLESS_URL with your self-hosted Paperless-ngx URL.")
	}
	u, e := url.Parse(c.base)
	if e != nil || u == nil || len(c.base) > 2048 || u.Opaque != "" || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.ContainsAny(c.base, "\\\r\n\t") || strings.Contains(u.EscapedPath(), "%") {
		return nil, errors.New("SOKEL_PAPERLESS_URL must be an HTTP or HTTPS service URL with an optional plain path prefix and no credentials, query or fragment.")
	}
	for _, seg := range strings.Split(u.Path, "/") {
		if seg == "." || seg == ".." {
			return nil, errors.New("Service URL path cannot contain dot segments.")
		}
	}
	if u.Port() != "" {
		n, e := strconv.Atoi(u.Port())
		if e != nil || n < 1 || n > 65535 {
			return nil, errors.New("Service URL has an invalid port.")
		}
	}
	raw := strings.TrimRight(u.EscapedPath(), "/") + route
	u.Path, e = url.PathUnescape(raw)
	if e != nil {
		return nil, errors.New("Invalid API path.")
	}
	u.RawPath = raw
	u.RawQuery = q.Encode()
	return u, nil
}
func safeText(s string, max int) bool {
	if len(s) > max || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if r < 32 || r == 127 {
			return false
		}
	}
	return true
}
func validBody(s string, max int) bool {
	return len(s) <= max && utf8.ValidString(s) && !strings.ContainsRune(s, 0)
}
func retryDelay(v string) int {
	if n, e := strconv.Atoi(v); e == nil && n > 0 && n <= 86400 {
		return n
	}
	if ts, e := http.ParseTime(v); e == nil {
		n := int(time.Until(ts).Seconds()) + 1
		if n > 0 && n <= 86400 {
			return n
		}
	}
	return 60
}
func (c *client) request(ctx context.Context, method, route string, q url.Values, body []byte, ct string, status int, cap int64) ([]byte, http.Header, error) {
	if e := ctx.Err(); e != nil {
		return nil, nil, e
	}
	u, e := c.endpoint(route, q)
	if e != nil {
		return nil, nil, e
	}
	if c.cred.Token == "" || !safeText(c.cred.Token, 4096) || strings.ContainsAny(c.cred.Token, " \t") {
		return nil, nil, errors.New("Provide a valid API token in the plugin credentials.")
	}
	req, e := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(body))
	if e != nil {
		return nil, nil, errors.New("Could not create the Paperless-ngx request.")
	}
	req.GetBody = nil
	req.Header.Set("Authorization", "Token "+c.cred.Token)
	req.Header.Set("Accept", "application/json; version=10")
	if ct != "" {
		req.Header.Set("Content-Type", ct)
	}
	h := *c.http
	h.Jar = nil
	h.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if h.Timeout <= 0 || h.Timeout > requestTimeout {
		h.Timeout = requestTimeout
	}
	res, e := h.Do(req)
	if e != nil {
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		return nil, nil, errors.New("Paperless-ngx transport failed or timed out. The plugin did not retry; check the server before repeating a write.")
	}
	defer res.Body.Close()
	if res.StatusCode != status {
		if res.StatusCode == 429 {
			return nil, nil, fmt.Errorf("Paperless-ngx is rate limited. Wait at least %d seconds; writes are not retried automatically.", retryDelay(res.Header.Get("Retry-After")))
		}
		return nil, nil, fmt.Errorf("Paperless-ngx returned HTTP %d. Check the service URL, token permissions and request parameters. No automatic retry was attempted.", res.StatusCode)
	}
	if v := res.Header.Get("X-Api-Version"); v != "" && v != "10" {
		return nil, nil, errors.New("Paperless-ngx must support API version 10.")
	}
	if res.ContentLength > cap {
		return nil, nil, errors.New("Paperless-ngx response exceeds the plugin size limit.")
	}
	b, e := io.ReadAll(io.LimitReader(res.Body, cap+1))
	if e != nil {
		return nil, nil, errors.New("Could not read the Paperless-ngx response.")
	}
	if int64(len(b)) > cap {
		return nil, nil, errors.New("Paperless-ngx response exceeds the plugin size limit.")
	}
	if res.ContentLength >= 0 && int64(len(b)) != res.ContentLength {
		return nil, nil, errors.New("Paperless-ngx response byte count does not match Content-Length.")
	}
	return b, res.Header, nil
}
func decodeJSON(b []byte, out any) error {
	if !utf8.Valid(b) {
		return errors.New("Paperless-ngx returned invalid UTF-8 JSON.")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	if d.Decode(out) != nil {
		return errors.New("Paperless-ngx returned malformed JSON.")
	}
	var x any
	if d.Decode(&x) != io.EOF {
		return errors.New("Paperless-ngx returned trailing JSON data.")
	}
	return nil
}
func (c *client) json(ctx context.Context, method, route string, q url.Values, body any, status int, out any) (http.Header, error) {
	var b []byte
	var e error
	ct := ""
	if body != nil {
		b, e = json.Marshal(body)
		if e != nil {
			return nil, errors.New("Could not encode the request.")
		}
		ct = "application/json"
	}
	b, h, e := c.request(ctx, method, route, q, b, ct, status, maxJSON)
	if e != nil {
		return nil, e
	}
	typ, _, e := mime.ParseMediaType(h.Get("Content-Type"))
	if e != nil || typ != "application/json" {
		return nil, errors.New("Paperless-ngx returned an unexpected response media type.")
	}
	return h, decodeJSON(b, out)
}
func positiveID(s string) (int64, error) {
	if s == "" || len(s) > 19 || strings.TrimLeft(s, "0123456789") != "" || s[0] == '0' {
		return 0, errors.New("IDs must be positive decimal integers without leading zeros.")
	}
	n, e := strconv.ParseInt(s, 10, 64)
	if e != nil || n < 1 {
		return 0, errors.New("ID is outside the supported positive 64-bit integer range.")
	}
	return n, nil
}
func idString(n int64) (string, error) {
	if n < 1 {
		return "", errors.New("Paperless-ngx returned an invalid object ID.")
	}
	return strconv.FormatInt(n, 10), nil
}

// Convert a trusted same-endpoint continuation to a page number. Never fetch or return its URL.
func (c *client) nextPage(raw, route string, q url.Values, page int) (int, error) {
	if raw == "" {
		return 0, nil
	}
	expected, e := c.endpoint(route, q)
	if e != nil {
		return 0, e
	}
	u, e := url.Parse(raw)
	if e != nil || u == nil || u.User != nil || u.Fragment != "" || u.Opaque != "" || u.Scheme != expected.Scheme || u.Host != expected.Host || u.EscapedPath() != expected.EscapedPath() {
		return 0, errors.New("Paperless-ngx returned an unsafe or unsupported pagination URL.")
	}
	v, e := url.ParseQuery(u.RawQuery)
	if e != nil || len(v["page"]) != 1 {
		return 0, errors.New("Paperless-ngx returned invalid pagination parameters.")
	}
	n, e := strconv.Atoi(v.Get("page"))
	if e != nil || n != page+1 || n > 100000 {
		return 0, errors.New("Paperless-ngx pagination did not advance to the next page.")
	}
	v.Del("page")
	want, _ := url.ParseQuery(q.Encode())
	want.Del("page")
	if v.Encode() != want.Encode() {
		return 0, errors.New("Paperless-ngx pagination changed the requested filters or page size.")
	}
	return n, nil
}
