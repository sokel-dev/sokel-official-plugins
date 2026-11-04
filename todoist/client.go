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

const apiOrigin = "https://api.todoist.com/api/v1"
const maxResponse = 8 << 20
const maxRequest = 1 << 20

type api struct {
	cred Cred
	http *http.Client
}

func newAPI(c Cred) *api { return &api{cred: c, http: &http.Client{Timeout: 50 * time.Second}} }
func (a *api) call(ctx context.Context, method, path string, q url.Values, body any, status int) ([]byte, error) {
	token := strings.TrimSpace(a.cred.AccessToken)
	if token == "" || strings.ContainsAny(token, "\r\n\t ") {
		return nil, errors.New("missing or invalid access token; update the plugin credential")
	}
	if !strings.HasPrefix(path, "/") || strings.ContainsAny(path, "?#\\") || strings.Contains(path, "..") {
		return nil, errors.New("invalid API route")
	}
	var raw []byte
	var err error
	if body != nil {
		raw, err = json.Marshal(body)
		if err != nil {
			return nil, errors.New("request cannot be encoded as JSON")
		}
		if len(raw) > maxRequest {
			return nil, errors.New("request exceeds the 1 MiB limit")
		}
	}
	target := apiOrigin + path
	if len(q) > 0 {
		target += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(raw))
	if err != nil {
		return nil, errors.New("cannot construct API request")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	c := *a.http
	c.Timeout = 50 * time.Second
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := c.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, errors.New("API request canceled or timed out")
		}
		return nil, errors.New("API connection failed; check network access")
	}
	defer res.Body.Close()
	raw, err = io.ReadAll(io.LimitReader(res.Body, maxResponse+1))
	if err != nil {
		return nil, errors.New("cannot read API response")
	}
	if len(raw) > maxResponse {
		return nil, errors.New("API response exceeds the 8 MiB limit; narrow the query")
	}
	if res.StatusCode != status {
		switch res.StatusCode {
		case 401, 403:
			return nil, errors.New("access denied; check token expiry, scopes and resource permissions")
		case 429:
			return nil, errors.New("API rate limit exceeded; wait before retrying; writes are never retried automatically")
		}
		return nil, fmt.Errorf("API returned HTTP %d", res.StatusCode)
	}
	if len(bytes.TrimSpace(raw)) > 0 && string(bytes.TrimSpace(raw)) != "null" {
		var envelope map[string]json.RawMessage
		if err := decode(raw, &envelope); err != nil {
			return nil, err
		}
		for _, key := range []string{"error", "err"} {
			if v, ok := envelope[key]; ok && string(v) != "null" && string(v) != "false" && string(v) != `""` {
				return nil, errors.New("API reported an error; verify request parameters and account permissions")
			}
		}
		if v, ok := envelope["code"]; ok {
			var code json.Number
			if json.Unmarshal(v, &code) != nil || string(code) != "0" {
				return nil, errors.New("API reported an error; verify request parameters and account permissions")
			}
		}
	}
	return raw, nil
}
func decode(raw []byte, out any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if d.Decode(out) != nil {
		return errors.New("API returned malformed JSON")
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return errors.New("API returned trailing JSON")
	}
	return nil
}

var idPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func validID(s string) bool {
	return len(s) > 0 && len(s) <= 200 && idPattern.MatchString(s) && !strings.HasPrefix(s, "tmp-")
}
func requireID(s string) error {
	if !validID(s) {
		return errors.New("invalid resource ID; use a confirmed ID, not a URL")
	}
	return nil
}
func pageQuery(limit, max int, cursor, key string) (url.Values, error) {
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > max {
		return nil, fmt.Errorf("page size must be between 1 and %d", max)
	}
	if len(cursor) > 4096 || strings.ContainsAny(cursor, "\r\n") {
		return nil, errors.New("invalid pagination token")
	}
	q := url.Values{"limit": {strconv.Itoa(limit)}}
	if cursor != "" {
		q.Set(key, cursor)
	}
	return q, nil
}
func requiredText(s string, max int) error {
	if strings.TrimSpace(s) == "" || len([]rune(s)) > max {
		return fmt.Errorf("text must contain between 1 and %d characters", max)
	}
	return nil
}
func badResponse() error { return errors.New("API response is missing required fields") }
