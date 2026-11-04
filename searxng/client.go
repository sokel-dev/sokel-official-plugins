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
	"os"
	"strings"
	"time"
)

const maxResponse = 8 << 20

type client struct {
	base string
	cred Cred
	http *http.Client
}

func newClient(cred Cred) *client {
	return &client{base: os.Getenv("SOKEL_SEARXNG_URL"), cred: cred, http: &http.Client{Timeout: 60 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func (c *client) endpoint(path string, query url.Values) (string, error) {
	base := strings.TrimSpace(c.base)
	u, e := url.Parse(base)
	if e != nil || u == nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.ContainsAny(base, "\\\r\n\t") || strings.Contains(u.EscapedPath(), "%") {
		return "", errors.New("set SOKEL_SEARXNG_URL to an HTTP(S) service URL without credentials, query or fragment")
	}
	for _, seg := range strings.Split(u.Path, "/") {
		if seg == "." || seg == ".." {
			return "", errors.New("SOKEL_SEARXNG_URL must not contain dot path segments")
		}
	}
	u.Path = strings.TrimRight(u.Path, "/") + path
	u.RawPath = ""
	u.RawQuery = query.Encode()
	return u.String(), nil
}
func (c *client) request(ctx context.Context, method, path string, query url.Values, body []byte, headers http.Header) ([]byte, http.Header, error) {
	target, e := c.endpoint(path, query)
	if e != nil {
		return nil, nil, e
	}
	if len(body) > 1<<20 {
		return nil, nil, errors.New("request exceeds the 1 MiB limit")
	}
	req, e := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(body))
	if e != nil {
		return nil, nil, errors.New("could not create service request")
	}
	if method != "GET" {
		req.GetBody = nil
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header[k] = v
	}
	if e = c.authorize(req); e != nil {
		return nil, nil, e
	}
	// Copy the caller's client so redirects are disabled even with an injected transport.
	hc := *c.http
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if hc.Timeout == 0 {
		hc.Timeout = 60 * time.Second
	}
	resp, e := hc.Do(req)
	if e != nil {
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		return nil, nil, errors.New("searxng request failed; check service connectivity and TLS configuration")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		hint := "request rejected"
		switch resp.StatusCode {
		case 401:
			hint = "verify the configured credentials"
		case 403:
			hint = "verify permissions and service API configuration"
		case 429:
			hint = "rate limit reached; retry later"
		}
		return nil, nil, fmt.Errorf("searxng HTTP %d: %s", resp.StatusCode, hint)
	}
	b, e := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if e != nil {
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		return nil, nil, errors.New("could not read the service response")
	}
	if len(b) > maxResponse {
		return nil, nil, errors.New("service response exceeds 8 MiB; narrow the request")
	}
	return b, resp.Header, nil
}
func decodeJSON(b []byte, h http.Header, out any) error {
	ct, _, e := mime.ParseMediaType(h.Get("Content-Type"))
	if e != nil || (ct != "application/json" && !strings.HasSuffix(ct, "+json")) {
		return errors.New("expected a JSON API response; check proxy authentication and service API settings")
	}
	if bytes.Equal(bytes.TrimSpace(b), []byte("null")) {
		return errors.New("service returned a null response")
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	if dec.Decode(out) != nil {
		return errors.New("service returned invalid JSON")
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		return errors.New("service returned trailing JSON data")
	}
	return nil
}
