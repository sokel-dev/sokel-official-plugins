package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const maxJSON = 4 << 20
const maxRequest = 1 << 20

type client struct {
	base string
	cred Cred
	http *http.Client
}
type apiError struct {
	Status     int
	RetryAfter time.Duration
}

func (e *apiError) Error() string {
	switch e.Status {
	case 400:
		return "Stripe rejected the parameters. Check resource IDs, property types and required fields."
	case 401:
		return "Stripe credentials were rejected. Renew the configured credential."
	case 403:
		return "Stripe denied access. Check the credential permissions and resource access."
	case 404:
		return "Stripe resource was not found or is not accessible."
	case 429:
		return fmt.Sprintf("Stripe rate limit reached; wait at least %d seconds. No automatic retry was made.", int(e.RetryAfter.Seconds()))
	}
	return fmt.Sprintf("Stripe returned HTTP %d. Requests are not automatically retried.", e.Status)
}
func newClient(cred Cred) *client {
	base := strings.TrimRight(os.Getenv("STRIPE_API_BASE_URL"), "/")
	if base == "" {
		base = "https://api.stripe.com"
	}
	return &client{base: base, cred: cred, http: &http.Client{Timeout: 45 * time.Second}}
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
func retryAfter(s string) time.Duration {
	if n, e := strconv.Atoi(s); e == nil && n > 0 && n <= 86400 {
		return time.Duration(n) * time.Second
	}
	if t, e := http.ParseTime(s); e == nil {
		d := time.Until(t)
		if d > 0 && d <= 24*time.Hour {
			return d.Round(time.Second) + time.Second
		}
	}
	return time.Minute
}
func (c *client) request(ctx context.Context, method, path string, q url.Values, data []byte, contentType string) ([]byte, http.Header, error) {
	if !validBase(c.base) {
		return nil, nil, errors.New("Configure STRIPE_API_BASE_URL as an HTTPS deployment endpoint; HTTP is limited to loopback tests.")
	}
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || strings.ContainsAny(path, "?#\r\n") {
		return nil, nil, errors.New("Invalid API request path.")
	}
	if len(data) > 12<<20 {
		return nil, nil, errors.New("Request exceeds 12 MiB.")
	}
	endpoint := c.base + path
	if len(q) > 0 {
		endpoint += "?" + q.Encode()
	}
	req, e := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(data))
	if e != nil {
		return nil, nil, errors.New("Invalid Stripe request configuration.")
	}
	key := strings.TrimSpace(c.cred.APIKey)
	if key == "" || strings.HasPrefix(key, "pk_") {
		return nil, nil, errors.New("Configure a Stripe secret or restricted API key with read permissions.")
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Stripe-Version", stripeVersion)
	req.Header.Set("Accept", "application/json")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
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
		return nil, nil, errors.New("Cannot reach Stripe. Check deployment networking and the configured endpoint.")
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, nil, &apiError{Status: res.StatusCode, RetryAfter: retryAfter(res.Header.Get("Retry-After"))}
	}
	b, e := io.ReadAll(io.LimitReader(res.Body, maxJSON+1))
	if e != nil {
		return nil, nil, errors.New("Failed to read the Stripe response.")
	}
	if len(b) > maxJSON {
		return nil, nil, errors.New("Stripe response exceeds 4 MiB. Narrow the query.")
	}
	return b, res.Header, nil
}
func decodeJSON(b []byte, out any) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if e := dec.Decode(out); e != nil {
		return errors.New("Stripe returned malformed JSON or an unexpected response shape.")
	}
	var more any
	if e := dec.Decode(&more); e != io.EOF {
		return errors.New("Stripe returned extra data after JSON.")
	}
	return nil
}
func (c *client) json(ctx context.Context, method, path string, q url.Values, body any, out any) (http.Header, error) {
	var b []byte
	var e error
	if body != nil {
		b, e = json.Marshal(body)
		if e != nil {
			return nil, errors.New("Request contains invalid JSON values.")
		}
		if len(b) > maxRequest {
			return nil, errors.New("JSON request exceeds 1 MiB.")
		}
	}
	b, h, e := c.request(ctx, method, path, q, b, "application/json")
	if e != nil {
		return nil, e
	}
	if e = decodeJSON(b, out); e != nil {
		return nil, e
	}
	return h, nil
}
