package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const apiOrigin = "https://api.browser-use.com"
const maxJSON = 4 << 20

type client struct {
	cred Cred
	http *http.Client
}

func newClient(cred Cred) *client {
	return &client{cred: cred, http: &http.Client{Timeout: 45 * time.Second}}
}

type apiError struct {
	Status     int
	RetryAfter time.Duration
}

func (e *apiError) Error() string {
	switch e.Status {
	case 401:
		return "Browser Use rejected the credential. Obtain or refresh it externally."
	case 403:
		return "Browser Use denied access. Check the credential, application permissions and resource access."
	case 404:
		return "The Browser Use resource was not found or is inaccessible."
	case 402:
		return "Browser Use rejected the request because of project funding or spending limits."
	case 409:
		return "Browser Use reported a conflict or busy session. Inspect current state before retrying."
	case 429:
		return fmt.Sprintf("Browser Use rate limit reached; wait at least %d seconds. No automatic retry was made.", int(e.RetryAfter.Seconds()))
	}
	return fmt.Sprintf("Browser Use returned HTTP %d. Inspect parameters and current state before retrying; no automatic retry was made.", e.Status)
}
func retryAfter(v string) time.Duration {
	if n, e := strconv.Atoi(v); e == nil && n > 0 && n <= 86400 {
		return time.Duration(n) * time.Second
	}
	if t, e := http.ParseTime(v); e == nil {
		d := time.Until(t)
		if d > 0 && d <= 24*time.Hour {
			return d.Round(time.Second) + time.Second
		}
	}
	return time.Minute
}
func tokenOK(s string, max int) bool {
	return strings.TrimSpace(s) != "" && len(s) <= max && !strings.ContainsAny(s, " \t\r\n\x00")
}
func cursorOK(s string) bool { return len(s) <= 8192 && !strings.ContainsAny(s, "\r\n\x00") }
func decode(b []byte, out any) error {
	if len(b) == 0 || bytes.Equal(bytes.TrimSpace(b), []byte("null")) {
		return errors.New("Browser Use returned an empty JSON response.")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	if e := d.Decode(out); e != nil {
		return errors.New("Browser Use returned malformed or incompatible JSON.")
	}
	var extra any
	if e := d.Decode(&extra); e != io.EOF {
		return errors.New("Browser Use returned trailing JSON data.")
	}
	return nil
}
func (c *client) request(ctx context.Context, method, path string, q url.Values, body any) ([]byte, error) {
	if !strings.HasPrefix(path, "/api/v4/") || strings.ContainsAny(path, "?#\\\r\n\x00") || strings.Contains(path, "..") || strings.Contains(path, "%") {
		return nil, errors.New("Invalid Browser Use resource path.")
	}
	target := apiOrigin + path
	if len(q) > 0 {
		target += "?" + q.Encode()
	}
	var b []byte
	var e error
	if body != nil {
		b, e = json.Marshal(body)
		if e != nil {
			return nil, errors.New("Request fields must be JSON-compatible.")
		}
		if len(b) > 1<<20 {
			return nil, errors.New("JSON request exceeds the 1 MiB plugin limit.")
		}
	}
	req, e := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(b))
	if e != nil {
		return nil, errors.New("Invalid Browser Use request configuration.")
	}
	req.GetBody = nil
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	if !tokenOK(c.cred.APIKey, 8192) {
		return nil, errors.New("Configure a valid Browser Use Cloud API key.")
	}
	req.Header.Set("X-Browser-Use-API-Key", c.cred.APIKey)
	h := *c.http
	if h.Timeout == 0 {
		h.Timeout = 45 * time.Second
	}
	h.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, e := h.Do(req)
	if e != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("Cannot reach Browser Use. A timed-out write may already have succeeded; inspect state before retrying.")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, &apiError{Status: res.StatusCode, RetryAfter: retryAfter(res.Header.Get("Retry-After"))}
	}
	data, e := io.ReadAll(io.LimitReader(res.Body, maxJSON+1))
	if e != nil {
		return nil, errors.New("Cannot read the Browser Use response.")
	}
	if len(data) > maxJSON {
		return nil, errors.New("Browser Use response exceeds the 4 MiB plugin limit. Reduce the requested page size or output.")
	}
	return data, nil
}
