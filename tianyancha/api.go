package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const jsonLimit int64 = 8 << 20

type api struct {
	cred   Cred
	client *http.Client
}

func newAPI(cred Cred) *api { return &api{cred: cred, client: &http.Client{Timeout: 55 * time.Second}} }

// Requests stay on the fixed service origin. There are no retries, including paid reads.
func (a *api) get(ctx context.Context, path string, q url.Values) ([]byte, error) {
	if strings.TrimSpace(a.cred.AccessToken) == "" || strings.ContainsAny(a.cred.AccessToken, "\r\n") {
		return nil, fmt.Errorf("missing or invalid Tianyancha token")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://open.api.tianyancha.com"+path+"?"+q.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("invalid Tianyancha request")
	}
	req.Header.Set("Authorization", a.cred.AccessToken)
	req.Header.Set("Accept", "application/json")
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
		return nil, fmt.Errorf("Tianyancha transport failed; check connectivity")
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		switch res.StatusCode {
		case 401:
			return nil, fmt.Errorf("Tianyancha credential invalid or expired")
		case 403:
			return nil, fmt.Errorf("Tianyancha denied access; check API permissions and account restrictions")
		case 429:
			return nil, fmt.Errorf("Tianyancha rate limit reached; retry later")
		}
		return nil, fmt.Errorf("Tianyancha returned HTTP %d", res.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, jsonLimit+1))
	if err != nil {
		return nil, fmt.Errorf("failed to read Tianyancha response")
	}
	if int64(len(raw)) > jsonLimit {
		return nil, fmt.Errorf("Tianyancha response exceeds 8 MiB")
	}
	if !json.Valid(raw) {
		return nil, fmt.Errorf("Tianyancha returned malformed JSON")
	}
	return raw, nil
}
