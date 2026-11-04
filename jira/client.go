package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const requestLimit = 4 << 20
const responseLimit = 8 << 20

type api struct {
	cred   Cred
	client *http.Client
}

func newAPI(c Cred) *api { return &api{cred: c, client: &http.Client{Timeout: 55 * time.Second}} }

type apiError struct{ Status int }

func (e *apiError) Error() string {
	switch e.Status {
	case 400, 422:
		return "Jira Cloud rejected request fields; check IDs, field configuration and input format"
	case 401:
		return "Jira Cloud authentication failed; check the credential and token expiration"
	case 403:
		return "Jira Cloud denied access; check token scopes and resource permissions"
	case 404:
		return "Jira Cloud resource was not found or is not accessible"
	case 409:
		return "Jira Cloud version conflict; fetch the latest resource and resolve changes before retrying"
	case 429:
		return "Jira Cloud rate limit reached; wait before retrying"
	}
	return fmt.Sprintf("Jira Cloud API returned HTTP %d", e.Status)
}
func (a *api) request(ctx context.Context, method, path string, q url.Values, input, out any) (http.Header, error) {
	base, err := a.base()
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || strings.ContainsAny(path, "?#\r\n") {
		return nil, fmt.Errorf("invalid API path")
	}
	var raw []byte
	if input != nil {
		raw, err = json.Marshal(input)
		if err != nil {
			return nil, fmt.Errorf("request contains invalid JSON values")
		}
		if len(raw) > requestLimit {
			return nil, fmt.Errorf("request exceeds 4 MiB")
		}
	}
	endpoint := base + path
	if len(q) > 0 {
		endpoint += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("invalid Jira Cloud request")
	}
	req.SetBasicAuth(a.cred.Email, a.cred.APIToken)
	req.Header.Set("Accept", "application/json")
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := *a.client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if client.Timeout == 0 {
		client.Timeout = 55 * time.Second
	}
	res, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("Jira Cloud request transport failed; check connectivity")
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, &apiError{Status: res.StatusCode}
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, responseLimit+1))
	if err != nil {
		return nil, fmt.Errorf("failed to read Jira Cloud response")
	}
	if len(b) > responseLimit {
		return nil, fmt.Errorf("Jira Cloud response exceeds 8 MiB")
	}
	if out != nil {
		if err = json.Unmarshal(b, out); err != nil {
			return nil, fmt.Errorf("Jira Cloud returned malformed JSON")
		}
	}
	return res.Header, nil
}
func pageSize(n int) (int, error) {
	if n == 0 {
		return 50, nil
	}
	if n < 1 || n > 100 {
		return 0, fmt.Errorf("page size must be between 1 and 100")
	}
	return n, nil
}
func required(s, name string) error {
	if strings.TrimSpace(s) == "" || strings.ContainsAny(s, "\r\n") {
		return fmt.Errorf("%s is required and must not contain line breaks", name)
	}
	return nil
}
func validDate(s string) error {
	if s == "" {
		return nil
	}
	d, e := time.Parse("2006-01-02", s)
	if e != nil || d.Format("2006-01-02") != s {
		return fmt.Errorf("date must use YYYY-MM-DD")
	}
	return nil
}

var cloudIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
var sitePattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.atlassian\.net$`)

func (a *api) base() (string, error) {
	if strings.TrimSpace(a.cred.APIToken) == "" || strings.ContainsAny(a.cred.APIToken, "\r\n") {
		return "", fmt.Errorf("missing or invalid Atlassian API token")
	}
	email, e := mail.ParseAddress(a.cred.Email)
	if e != nil || email.Address != a.cred.Email || strings.Contains(a.cred.Email, ":") {
		return "", fmt.Errorf("enter a plain Atlassian account email address")
	}
	if a.cred.CloudID != "" {
		if !cloudIDPattern.MatchString(a.cred.CloudID) {
			return "", fmt.Errorf("Cloud ID must be a UUID")
		}
		return "https://api.atlassian.com/ex/jira/" + a.cred.CloudID, nil
	}
	raw := strings.TrimSpace(a.cred.Site)
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || u.RawPath != "" || !sitePattern.MatchString(u.Host) {
		return "", fmt.Errorf("site must be an HTTPS *.atlassian.net origin without credentials, port, path or query")
	}
	return "https://" + u.Host, nil
}
func numericID(s, name string) (string, error) {
	if !regexp.MustCompile(`^[1-9][0-9]*$`).MatchString(s) {
		return "", fmt.Errorf("%s must be a positive numeric ID", name)
	}
	return s, nil
}
