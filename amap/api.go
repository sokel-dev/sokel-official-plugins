package main

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
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
	if strings.TrimSpace(a.cred.APIKey) == "" || strings.ContainsAny(a.cred.APIKey, "\r\n") {
		return nil, fmt.Errorf("missing or invalid AMap API key")
	}
	q.Set("key", a.cred.APIKey)
	q.Set("output", "JSON")
	if a.cred.SigningSecret != "" {
		q.Set("sig", signature(q, a.cred.SigningSecret))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://restapi.amap.com"+path+"?"+q.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("invalid AMap request")
	}
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
		return nil, fmt.Errorf("AMap transport failed; check connectivity")
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		switch res.StatusCode {
		case 401:
			return nil, fmt.Errorf("AMap credential invalid or expired")
		case 403:
			return nil, fmt.Errorf("AMap denied access; check API permissions and account restrictions")
		case 429:
			return nil, fmt.Errorf("AMap rate limit reached; retry later")
		}
		return nil, fmt.Errorf("AMap returned HTTP %d", res.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, jsonLimit+1))
	if err != nil {
		return nil, fmt.Errorf("failed to read AMap response")
	}
	if int64(len(raw)) > jsonLimit {
		return nil, fmt.Errorf("AMap response exceeds 8 MiB")
	}
	if !json.Valid(raw) {
		return nil, fmt.Errorf("AMap returned malformed JSON")
	}
	return raw, nil
}

// AMap signs sorted unescaped parameters, then URL-encodes the signed query.
func signature(q url.Values, secret string) string {
	keys := make([]string, 0, len(q))
	for k := range q {
		if k != "sig" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+q.Get(k))
	}
	sum := md5.Sum([]byte(strings.Join(parts, "&") + secret))
	return hex.EncodeToString(sum[:])
}
