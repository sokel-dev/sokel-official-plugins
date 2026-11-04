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
)

const maxRequest = 1 << 20
const maxResponse = 8 << 20

type client struct {
	cred Cred
	http *http.Client
}

func newClient(cred Cred) *client {
	return &client{cred: cred, http: &http.Client{Timeout: 45 * time.Second}}
}

type apiError struct {
	Status     int
	RetryAfter int
}

func (e *apiError) Error() string {
	switch e.Status {
	case 401:
		return "Monday rejected the credentials. Replace the token outside this plugin."
	case 403:
		return "Monday denied access. Check token scopes and resource permissions."
	case 404:
		return "The Monday resource was not found or is not accessible."
	case 429:
		return fmt.Sprintf("Monday rate limit reached. Wait at least %d seconds before another attempt.", e.RetryAfter)
	default:
		return fmt.Sprintf("Monday returned HTTP %d. Inspect any write result before retrying.", e.Status)
	}
}
func retryAfter(s string) int {
	if n, e := strconv.Atoi(s); e == nil && n > 0 && n <= 86400 {
		return n
	}
	if t, e := http.ParseTime(s); e == nil {
		n := int(time.Until(t).Seconds()) + 1
		if n > 0 && n <= 86400 {
			return n
		}
	}
	return 60
}
func safeToken(s string) bool {
	if len(s) == 0 || len(s) > 16384 {
		return false
	}
	for _, c := range s {
		if c < 33 || c > 126 {
			return false
		}
	}
	return true
}
func decodeJSON(b []byte, out any) error {
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	if e := d.Decode(out); e != nil {
		return errors.New("Monday returned malformed JSON.")
	}
	var extra any
	if e := d.Decode(&extra); e != io.EOF {
		return errors.New("Monday returned trailing JSON data.")
	}
	return nil
}
func (c *client) request(ctx context.Context, method, path string, q url.Values, input any) ([]byte, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || strings.ContainsAny(path, "?#\\\r\n\x00") || strings.Contains(path, "/../") || strings.Contains(path, "/./") {
		return nil, errors.New("Invalid Monday resource path.")
	}
	auth, e := c.authorization()
	if e != nil {
		return nil, e
	}
	origin, e := c.origin()
	if e != nil {
		return nil, e
	}
	target := origin + path
	if len(q) > 0 {
		target += "?" + q.Encode()
	}
	var raw []byte
	if input != nil {
		raw, e = json.Marshal(input)
		if e != nil {
			return nil, errors.New("Request contains invalid JSON values.")
		}
		if len(raw) > maxRequest {
			return nil, errors.New("Request exceeds the 1 MiB limit.")
		}
	}
	req, e := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(raw))
	if e != nil {
		return nil, errors.New("Invalid Monday request.")
	}
	c.setHeaders(req, auth)
	req.Header.Set("Accept", "application/json")
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	h := *c.http
	h.Jar = nil
	h.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if h.Timeout <= 0 || h.Timeout > 45*time.Second {
		h.Timeout = 45 * time.Second
	}
	res, e := h.Do(req)
	if e != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("Monday transport failed or timed out. Inspect any write result before retrying.")
	}
	defer res.Body.Close()
	want := 200

	if res.StatusCode != want {
		return nil, &apiError{Status: res.StatusCode, RetryAfter: retryAfter(res.Header.Get("Retry-After"))}
	}
	ct, _, e := mime.ParseMediaType(res.Header.Get("Content-Type"))
	if e != nil || ct != "application/json" {
		return nil, errors.New("Monday returned a non-JSON response.")
	}
	b, e := io.ReadAll(io.LimitReader(res.Body, maxResponse+1))
	if e != nil {
		return nil, errors.New("Could not read the Monday response.")
	}
	if len(b) > maxResponse {
		return nil, errors.New("Monday response exceeds the 8 MiB limit.")
	}
	if len(bytes.TrimSpace(b)) == 0 {
		return nil, errors.New("Monday returned an empty response.")
	}
	return b, nil
}
func (c *client) json(ctx context.Context, method, path string, q url.Values, input, out any) error {
	b, e := c.request(ctx, method, path, q, input)
	if e != nil {
		return e
	}
	return decodeJSON(b, out)
}
func requireKeys(b []byte, keys ...string) error {
	var obj map[string]json.RawMessage
	if e := decodeJSON(b, &obj); e != nil {
		return e
	}
	for _, k := range keys {
		v, ok := obj[k]
		if !ok || string(v) == "null" {
			return errors.New("Monday returned incomplete resource metadata.")
		}
	}
	return nil
}
