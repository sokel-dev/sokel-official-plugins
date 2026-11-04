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
	"regexp"
	"strconv"
	"strings"
	"time"
)

const graphOrigin = "https://graph.microsoft.com"
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
	case 400:
		return "Microsoft Graph rejected the request. Check resource IDs, permissions and field values."
	case 401:
		return "The delegated access token is missing, expired or invalid. Refresh it externally and update this credential."
	case 403:
		return "Microsoft Graph denied access. Check delegated scopes and the signed-in user's resource permissions."
	case 404:
		return "The Microsoft Graph resource was not found or is not accessible."
	case 409:
		return "The resource conflicts with the requested change. Inspect the resource before retrying."
	case 410:
		return "The delta checkpoint expired. Start a fresh enumeration and reconcile local state."
	case 412:
		return "The resource changed since the supplied ETag. Read the current version before updating."
	case 429:
		return fmt.Sprintf("Microsoft Graph rate limit reached; wait at least %d seconds. No automatic retry was made.", int(e.RetryAfter.Seconds()))
	}
	return fmt.Sprintf("Microsoft Graph returned HTTP %d. Requests are not automatically retried; inspect write results before retrying.", e.Status)
}
func retryAfter(v string) time.Duration {
	if n, e := strconv.Atoi(v); e == nil && n > 0 && n <= 86400 {
		return time.Duration(n) * time.Second
	}
	if ts, e := http.ParseTime(v); e == nil {
		d := time.Until(ts)
		if d > 0 && d <= 24*time.Hour {
			return d.Round(time.Second) + time.Second
		}
	}
	return time.Minute
}

var resourceID = regexp.MustCompile(`^[A-Za-z0-9_!.,{}-]{1,512}$`)

func segment(s string) (string, error) {
	if !resourceID.MatchString(s) || s == "." || s == ".." {
		return "", errors.New("Provide a valid resource ID without paths, query strings or URL escapes.")
	}
	return url.PathEscape(s), nil
}
func textSegment(s string) (string, error) {
	if strings.TrimSpace(s) == "" || len(s) > 255 || strings.ContainsAny(s, "/\\\x00\r\n?#%") || s == "." || s == ".." {
		return "", errors.New("Provide a resource ID or name without paths, URL escapes or control characters.")
	}
	return url.PathEscape(s), nil
}
func safeHeader(s string) bool { return len(s) <= 2048 && !strings.ContainsAny(s, "\r\n\x00") }
func graphURL(raw string) bool {
	u, e := url.Parse(raw)
	return e == nil && u.Scheme == "https" && u.Host == "graph.microsoft.com" && u.User == nil && u.Fragment == "" && u.Opaque == "" && strings.HasPrefix(u.Path, "/v1.0/") && !strings.ContainsAny(u.Path, "\r\n\x00\\")
}
func pageURL(path, link string, q url.Values) (string, error) {
	if link == "" {
		u := graphOrigin + path
		if len(q) > 0 {
			u += "?" + q.Encode()
		}
		if !graphURL(u) {
			return "", errors.New("Invalid Microsoft Graph resource path.")
		}
		return u, nil
	}
	u, e := url.Parse(link)
	decoded, de := url.PathUnescape(path)
	if len(link) > 16384 || e != nil || de != nil || !graphURL(link) || u.Path != decoded {
		return "", errors.New("Continuation URL must use the global Microsoft Graph origin and the same resource path.")
	}
	return link, nil
}
func checkedNext(path, current, next string) (string, error) {
	if next == "" {
		return "", nil
	}
	if next == current {
		return "", errors.New("Microsoft Graph repeated the current pagination URL.")
	}
	return pageURL(path, next, nil)
}
func (c *client) send(ctx context.Context, method, target string, b []byte, headers http.Header, statuses ...int) ([]byte, http.Header, error) {
	if !graphURL(target) {
		return nil, nil, errors.New("Only the global Microsoft Graph v1.0 origin is allowed.")
	}
	if strings.TrimSpace(c.cred.AccessToken) == "" || len(c.cred.AccessToken) > 32768 || strings.ContainsAny(c.cred.AccessToken, " \t\r\n\x00") {
		return nil, nil, errors.New("Configure a delegated Microsoft Graph access token; obtain and refresh it externally.")
	}
	if len(b) > 4<<20 {
		return nil, nil, errors.New("Request exceeds 4 MiB.")
	}
	req, e := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(b))
	if e != nil {
		return nil, nil, errors.New("Invalid Microsoft Graph request configuration.")
	}
	req.Header.Set("Authorization", "Bearer "+c.cred.AccessToken)
	req.Header.Set("Accept", "application/json")
	for k, vs := range headers {
		for _, v := range vs {
			if !safeHeader(v) {
				return nil, nil, errors.New("Invalid request header value.")
			}
			req.Header.Add(k, v)
		}
	}
	h := http.Client{Timeout: 45 * time.Second}
	if c.http != nil {
		h = *c.http
	}
	if h.Timeout == 0 {
		h.Timeout = 45 * time.Second
	}
	h.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, e := h.Do(req)
	if e != nil {
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		return nil, nil, errors.New("Cannot reach Microsoft Graph. Check deployment networking.")
	}
	defer res.Body.Close()
	accepted := false
	for _, s := range statuses {
		if res.StatusCode == s {
			accepted = true
		}
	}
	if !accepted {
		return nil, nil, &apiError{Status: res.StatusCode, RetryAfter: retryAfter(res.Header.Get("Retry-After"))}
	}
	raw, e := io.ReadAll(io.LimitReader(res.Body, maxJSON+1))
	if e != nil {
		return nil, nil, errors.New("Failed to read the Microsoft Graph response.")
	}
	if len(raw) > maxJSON {
		return nil, nil, errors.New("Microsoft Graph response exceeds 4 MiB. Narrow the query.")
	}
	return raw, res.Header, nil
}
func decodeJSON(raw []byte, out any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if d.Decode(out) != nil {
		return errors.New("Microsoft Graph returned malformed JSON or an unexpected response shape.")
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return errors.New("Microsoft Graph returned extra data after JSON.")
	}
	return nil
}
func (c *client) json(ctx context.Context, method, target string, body any, headers http.Header, out any, statuses ...int) error {
	var raw []byte
	var e error
	if body != nil {
		raw, e = json.Marshal(body)
		if e != nil {
			return errors.New("Request contains invalid JSON values.")
		}
		if len(raw) > 1<<20 {
			return errors.New("JSON request exceeds 1 MiB.")
		}
	}
	h := headers.Clone()
	if h == nil {
		h = make(http.Header)
	}
	h.Set("Content-Type", "application/json")
	raw, _, e = c.send(ctx, method, target, raw, h, statuses...)
	if e != nil {
		return e
	}
	return decodeJSON(raw, out)
}
func (c *client) healthCheck(ctx context.Context, _ *HealthCheckIn) (*HealthCheckOut, error) {
	var user struct {
		ID string `json:"id"`
	}
	e := c.json(ctx, "GET", graphOrigin+"/v1.0/me?$select=id", nil, nil, &user, 200)
	if e != nil {
		return &HealthCheckOut{Message: e.Error()}, nil
	}
	if user.ID == "" {
		return &HealthCheckOut{Message: "Microsoft Graph did not identify a signed-in user."}, nil
	}
	return &HealthCheckOut{OK: true, Message: "Delegated user identity verified. Resource scopes and write permissions are checked by each operation."}, nil
}
