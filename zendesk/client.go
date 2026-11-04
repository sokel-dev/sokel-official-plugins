package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const jsonLimit int64 = 8 << 20

type api struct {
	cred   Cred
	client *http.Client
}

func newAPI(c Cred) *api { return &api{cred: c, client: &http.Client{Timeout: 55 * time.Second}} }
func decodeJSON(raw []byte, out any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if e := d.Decode(out); e != nil {
		return fmt.Errorf("Zendesk returned malformed JSON")
	}
	if d.Decode(new(any)) != io.EOF {
		return fmt.Errorf("Zendesk returned trailing JSON content")
	}
	return nil
}
func (a *api) request(ctx context.Context, method, path string, q url.Values, body, out any) (int, error) {
	origin, e := a.origin()
	if e != nil {
		return 0, e
	}
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || strings.ContainsAny(path, "?#\r\n") {
		return 0, fmt.Errorf("invalid API path")
	}
	var raw []byte
	if body != nil {
		raw, e = json.Marshal(body)
		if e != nil {
			return 0, fmt.Errorf("request contains invalid JSON")
		}
		if int64(len(raw)) > jsonLimit {
			return 0, fmt.Errorf("JSON request exceeds 8 MiB")
		}
	}
	target := origin + path
	if len(q) > 0 {
		target += "?" + q.Encode()
	}
	req, e := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(raw))
	if e != nil {
		return 0, fmt.Errorf("invalid Zendesk request")
	}
	a.authorize(req)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := *a.client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if client.Timeout <= 0 {
		client.Timeout = 55 * time.Second
	}
	res, e := client.Do(req)
	if e != nil {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		return 0, fmt.Errorf("Zendesk transport failed; check connectivity")
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		switch res.StatusCode {
		case 401:
			return res.StatusCode, fmt.Errorf("Zendesk credential is invalid or expired; replace the token")
		case 403:
			return res.StatusCode, fmt.Errorf("Zendesk denied access; check API permissions and resource access")
		case 409, 412:
			return res.StatusCode, fmt.Errorf("Zendesk reported a concurrent update; read the latest record before trying again")
		case 429:
			return res.StatusCode, fmt.Errorf("Zendesk rate limit reached; delay subsequent requests")
		}
		return res.StatusCode, fmt.Errorf("Zendesk API returned HTTP %d", res.StatusCode)
	}

	raw, e = io.ReadAll(io.LimitReader(res.Body, jsonLimit+1))
	if e != nil {
		return res.StatusCode, fmt.Errorf("failed to read Zendesk response")
	}
	if int64(len(raw)) > jsonLimit {
		return res.StatusCode, fmt.Errorf("Zendesk response exceeds 8 MiB")
	}
	if out == nil {
		if len(bytes.TrimSpace(raw)) != 0 {
			return res.StatusCode, fmt.Errorf("Zendesk returned an unexpected response body")
		}
		return res.StatusCode, nil
	}
	if e = decodeJSON(raw, out); e != nil {
		return res.StatusCode, e
	}
	return res.StatusCode, nil
}
func validToken(s string) bool { return strings.TrimSpace(s) != "" && !strings.ContainsAny(s, "\r\n") }

var subdomainName = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

func (a *api) origin() (string, error) {
	if !subdomainName.MatchString(a.cred.Subdomain) || !validToken(a.cred.APIToken) || !strings.Contains(a.cred.Email, "@") || strings.ContainsAny(a.cred.Email, ":/ \r\n") {
		return "", fmt.Errorf("provide a Zendesk subdomain, agent email and API token")
	}
	return "https://" + a.cred.Subdomain + ".zendesk.com", nil
}
func (a *api) authorize(r *http.Request) { r.SetBasicAuth(a.cred.Email+"/token", a.cred.APIToken) }
