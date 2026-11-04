package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
	"unicode"
)

const maxFile = 32 << 20
const maxXML = 8 << 20

type client struct {
	base string
	cred Cred
	http *http.Client
}

func newClient(cred Cred) *client {
	return &client{base: os.Getenv("SOKEL_NEXTCLOUD_URL"), cred: cred, http: &http.Client{Timeout: 120 * time.Second}}
}
func safePath(s string, root bool) (string, error) {
	if strings.ContainsAny(s, "\\%") || strings.Contains(s, "//") || strings.Contains(s, "://") || strings.IndexFunc(s, unicode.IsControl) >= 0 {
		return "", errors.New("paths must be decoded relative paths without percent escapes, backslashes or control characters")
	}
	s = strings.TrimPrefix(s, "/")
	s = strings.TrimSuffix(s, "/")
	if s == "" {
		if root {
			return "", nil
		}
		return "", errors.New("a non-root path is required")
	}
	for _, part := range strings.Split(s, "/") {
		if part == "" || part == "." || part == ".." {
			return "", errors.New("empty or dot path segments are not allowed")
		}
	}
	if len(s) > 4096 {
		return "", errors.New("path exceeds 4096 bytes")
	}
	return s, nil
}
func (c *client) endpoint(p string) (*url.URL, error) {
	base := strings.TrimSpace(c.base)
	u, e := url.Parse(base)
	if e != nil || u == nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.ContainsAny(base, "\\\r\n\t") || strings.Contains(u.EscapedPath(), "%") {
		return nil, errors.New("set SOKEL_NEXTCLOUD_URL to the installation HTTP(S) URL without credentials, query, fragment or escaped path segments")
	}
	for _, s := range strings.Split(u.Path, "/") {
		if s == "." || s == ".." {
			return nil, errors.New("installation URL must not contain dot segments")
		}
	}
	user := c.cred.UserID
	if user == "" || user == "." || user == ".." || strings.ContainsAny(user, "/:\\%") || strings.IndexFunc(user, unicode.IsControl) >= 0 {
		return nil, errors.New("provide a canonical Nextcloud user ID without path separators or a colon")
	}
	if c.cred.AppPassword == "" || strings.IndexFunc(c.cred.AppPassword, unicode.IsControl) >= 0 {
		return nil, errors.New("a valid Nextcloud app password is required")
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/remote.php/dav/files/" + user + "/" + p
	u.RawPath = ""
	return u, nil
}
func (c *client) request(ctx context.Context, method, p string, body []byte, headers http.Header, limit int, statuses ...int) ([]byte, http.Header, int, error) {
	u, e := c.endpoint(p)
	if e != nil {
		return nil, nil, 0, e
	}
	req, e := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(body))
	if e != nil {
		return nil, nil, 0, errors.New("could not create WebDAV request")
	}
	req.GetBody = nil
	req.SetBasicAuth(c.cred.UserID, c.cred.AppPassword)
	for k, v := range headers {
		req.Header[k] = v
	}
	hc := *c.http
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if hc.Timeout == 0 {
		hc.Timeout = 120 * time.Second
	}
	resp, e := hc.Do(req)
	if e != nil {
		if ctx.Err() != nil {
			return nil, nil, 0, ctx.Err()
		}
		return nil, nil, 0, errors.New("Nextcloud request failed; check service connectivity and TLS configuration")
	}
	defer resp.Body.Close()
	ok := false
	for _, s := range statuses {
		if s == resp.StatusCode {
			ok = true
		}
	}
	if !ok {
		hint := "request rejected"
		switch resp.StatusCode {
		case 301, 302, 307, 308:
			hint = "redirects are disabled; use the final installation URL"
		case 401, 403:
			hint = "check user ID, app password and permissions"
		case 404:
			hint = "resource or WebDAV endpoint was not found"
		case 409:
			hint = "parent directory does not exist or the operation conflicts"
		case 412:
			hint = "destination already exists or the expected ETag no longer matches"
		case 423:
			hint = "resource is locked"
		}
		return nil, nil, 0, fmt.Errorf("Nextcloud HTTP %d: %s", resp.StatusCode, hint)
	}
	if resp.ContentLength > int64(limit) {
		return nil, nil, 0, errors.New("Nextcloud response exceeds the operation size limit")
	}
	b, e := io.ReadAll(io.LimitReader(resp.Body, int64(limit)+1))
	if e != nil {
		if ctx.Err() != nil {
			return nil, nil, 0, ctx.Err()
		}
		return nil, nil, 0, errors.New("could not read Nextcloud response")
	}
	if len(b) > limit {
		return nil, nil, 0, errors.New("Nextcloud response exceeds the operation size limit")
	}
	return b, resp.Header, resp.StatusCode, nil
}
func expectedETag(s string) error {
	if s == "" {
		return nil
	}
	if len(s) < 2 || len(s) > 1024 || s[0] != '"' || s[len(s)-1] != '"' || strings.ContainsAny(s[1:len(s)-1], "\"\r\n") || strings.IndexFunc(s, unicode.IsControl) >= 0 {
		return errors.New("expected ETag must be a single quoted strong ETag from resource metadata")
	}
	return nil
}
