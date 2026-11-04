package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const maxInput = 32 << 20
const timeout = 240 * time.Second

type client struct {
	base string
	cred Cred
	http *http.Client
}

func newClient(base string, cred Cred) *client {
	return &client{base: base, cred: cred, http: &http.Client{Timeout: timeout}}
}
func (c *client) endpoint(route string) (string, error) {
	if c.base == "" {
		return "", errors.New("Configure SOKEL_GOTENBERG_URL with your self-hosted Gotenberg URL.")
	}
	u, e := url.Parse(c.base)
	if e != nil || len(c.base) > 2048 || u == nil || u.Opaque != "" || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.ContainsAny(c.base, "\\\r\n\t") || strings.Contains(u.EscapedPath(), "%") {
		return "", errors.New("SOKEL_GOTENBERG_URL must be an HTTP or HTTPS service URL with an optional plain path prefix and no credentials, query or fragment.")
	}
	for _, seg := range strings.Split(u.Path, "/") {
		if seg == "." || seg == ".." {
			return "", errors.New("Service URL path cannot contain dot segments.")
		}
	}
	if u.Port() != "" {
		n, e := strconv.Atoi(u.Port())
		if e != nil || n < 1 || n > 65535 {
			return "", errors.New("Service URL has an invalid port.")
		}
	}
	u.Path = strings.TrimRight(u.Path, "/") + route
	return u.String(), nil
}
func safeText(s string, max int) bool {
	if !utf8.ValidString(s) || len(s) > max {
		return false
	}
	for _, r := range s {
		if r < 32 || r == 127 {
			return false
		}
	}
	return true
}
func (c *client) auth(req *http.Request) error {
	if c.cred.Username == "" && c.cred.Password == "" {
		return nil
	}
	if c.cred.Username == "" || c.cred.Password == "" || strings.Contains(c.cred.Username, ":") || !safeText(c.cred.Username, 256) || !safeText(c.cred.Password, 4096) {
		return errors.New("Provide both a valid Basic auth username and password, or leave both blank.")
	}
	req.SetBasicAuth(c.cred.Username, c.cred.Password)
	return nil
}
func retryDelay(value string) int {
	if n, e := strconv.Atoi(value); e == nil && n > 0 && n <= 86400 {
		return n
	}
	if ts, e := http.ParseTime(value); e == nil {
		n := int(time.Until(ts).Seconds()) + 1
		if n > 0 && n <= 86400 {
			return n
		}
	}
	return 60
}
func (c *client) request(ctx context.Context, method, route string, body []byte, headers http.Header, cap int64) ([]byte, http.Header, error) {
	if e := ctx.Err(); e != nil {
		return nil, nil, e
	}
	target, e := c.endpoint(route)
	if e != nil {
		return nil, nil, e
	}
	req, e := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(body))
	if e != nil {
		return nil, nil, errors.New("Could not create the Gotenberg request.")
	}
	req.GetBody = nil
	req.Header = headers.Clone()
	if req.Header == nil {
		req.Header = make(http.Header)
	}
	if e = c.auth(req); e != nil {
		return nil, nil, e
	}
	h := *c.http
	h.Jar = nil
	h.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if h.Timeout <= 0 || h.Timeout > timeout {
		h.Timeout = timeout
	}
	res, e := h.Do(req)
	if e != nil {
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		return nil, nil, errors.New("Gotenberg transport failed or timed out. Check the configured service; this request was not retried.")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		if res.StatusCode == 429 {
			return nil, nil, fmt.Errorf("Gotenberg is busy or rate limited. Wait at least %d seconds before another attempt.", retryDelay(res.Header.Get("Retry-After")))
		}
		return nil, nil, fmt.Errorf("Gotenberg returned HTTP %d. Check authentication, enabled modules and server limits.", res.StatusCode)
	}
	if res.ContentLength > cap {
		return nil, nil, errors.New("Gotenberg response exceeds the plugin size limit.")
	}
	b, e := io.ReadAll(io.LimitReader(res.Body, cap+1))
	if e != nil {
		return nil, nil, errors.New("Could not read the Gotenberg response.")
	}
	if int64(len(b)) > cap {
		return nil, nil, errors.New("Gotenberg response exceeds the plugin size limit.")
	}
	if res.ContentLength >= 0 && int64(len(b)) != res.ContentLength {
		return nil, nil, errors.New("Gotenberg response byte count does not match Content-Length.")
	}
	return b, res.Header, nil
}
func decodeJSON(b []byte, out any) error {
	if !utf8.Valid(b) {
		return errors.New("Gotenberg returned invalid UTF-8 JSON.")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	if d.Decode(out) != nil {
		return errors.New("Gotenberg returned malformed JSON.")
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return errors.New("Gotenberg returned trailing JSON data.")
	}
	return nil
}
func mediaType(h http.Header, want string) error {
	typ, _, e := mime.ParseMediaType(h.Get("Content-Type"))
	if e != nil || typ != want {
		return errors.New("Gotenberg returned an unexpected response media type.")
	}
	return nil
}
func validFilename(s string) bool {
	return s != "" && s != "." && s != ".." && safeText(s, 255) && !strings.ContainsAny(s, "/\\")
}
func quotedFilename(s string) string {
	return strings.NewReplacer("\\", "\\\\", "\"", "\\\"").Replace(s)
}
func readFile(ctx plugin.Ctx, f *plugin.File) ([]byte, error) {
	if f == nil || f.Size < 0 || f.Size > maxInput || (!validFilename(f.Name)) {
		return nil, errors.New("Provide a platform file with a plain filename and at most 32 MiB.")
	}
	if f.ID == "" && len(f.Data) == 0 {
		return nil, errors.New("Provide a platform file ID; local paths and external download URLs are not supported.")
	}
	b, e := f.Blob(ctx)
	if e != nil {
		return nil, errors.New("Could not read the platform file.")
	}
	if len(b) > maxInput {
		return nil, errors.New("Platform file exceeds 32 MiB.")
	}
	if f.Size > 0 && int64(len(b)) != f.Size {
		return nil, errors.New("Platform file byte count does not match its declared size.")
	}
	return b, nil
}
