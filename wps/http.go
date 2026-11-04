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

const apiOrigin = "https://openapi.wps.cn"
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
		return "WPS365 rejected the credential. Obtain or refresh it externally."
	case 403:
		return "WPS365 denied access. Check the credential, application permissions and resource access."
	case 404:
		return "The WPS365 resource was not found or is inaccessible."
	case 402:
		return "WPS365 rejected the request because of project funding or spending limits."
	case 409:
		return "WPS365 reported a conflict or busy session. Inspect current state before retrying."
	case 429:
		return fmt.Sprintf("WPS365 rate limit reached; wait at least %d seconds. No automatic retry was made.", int(e.RetryAfter.Seconds()))
	}
	return fmt.Sprintf("WPS365 returned HTTP %d. Inspect parameters and current state before retrying; no automatic retry was made.", e.Status)
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
		return errors.New("WPS365 returned an empty JSON response.")
	}
	if e := json.Unmarshal(b, out); e != nil {
		return errors.New("WPS365 returned malformed or incompatible JSON.")
	}
	return nil
}
func (c *client) request(ctx context.Context, method, path string, q url.Values, body any) ([]byte, error) {
	if !strings.HasPrefix(path, "/v7/") || strings.ContainsAny(path, "?#\\\r\n\x00") || strings.Contains(path, "..") || strings.Contains(path, "%") {
		return nil, errors.New("Invalid WPS365 resource path.")
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
		return nil, errors.New("Invalid WPS365 request configuration.")
	}
	req.GetBody = nil
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	if !tokenOK(c.cred.AccessToken, 32768) {
		return nil, errors.New("Configure a valid WPS365 access token; obtain and refresh it externally.")
	}
	req.Header.Set("Authorization", "Bearer "+c.cred.AccessToken)
	if (c.cred.AppID == "") != (c.cred.AppKey == "") {
		return nil, errors.New("Set both WPS signing fields, or leave both empty when API signing is disabled.")
	}
	if c.cred.AppID != "" {
		if !tokenOK(c.cred.AppID, 256) || strings.Contains(c.cred.AppID, ":") || !tokenOK(c.cred.AppKey, 8192) {
			return nil, errors.New("Invalid WPS application signing fields.")
		}
		date := time.Now().UTC().Format(http.TimeFormat)
		req.Header.Set("X-Kso-Date", date)
		req.Header.Set("X-Kso-Authorization", sign(c.cred.AppID, c.cred.AppKey, method, req.URL.RequestURI(), date, b))
	}
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
		return nil, errors.New("Cannot reach WPS365. A timed-out write may already have succeeded; inspect state before retrying.")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, &apiError{Status: res.StatusCode, RetryAfter: retryAfter(res.Header.Get("Retry-After"))}
	}
	data, e := io.ReadAll(io.LimitReader(res.Body, maxJSON+1))
	if e != nil {
		return nil, errors.New("Cannot read the WPS365 response.")
	}
	if len(data) > maxJSON {
		return nil, errors.New("WPS365 response exceeds the 4 MiB plugin limit. Reduce the requested page size or output.")
	}
	return data, nil
}
