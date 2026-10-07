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
	"sync"
	"time"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
)

// endpoint is a variable so tests can point it at a recording server.
var endpoint = "https://api.producthunt.com/v2/api/graphql"

var (
	clientsMu sync.Mutex
	clients   = map[string]*http.Client{}
)

func clientFor(proxy string) *http.Client {
	proxy = strings.TrimSpace(proxy)
	clientsMu.Lock()
	defer clientsMu.Unlock()
	if c, ok := clients[proxy]; ok {
		return c
	}
	c := &http.Client{Timeout: 30 * time.Second}
	if proxy != "" {
		if u, err := url.Parse(proxy); err == nil {
			tr := http.DefaultTransport.(*http.Transport).Clone()
			tr.Proxy = http.ProxyURL(u)
			c.Transport = tr
		}
	}
	clients[proxy] = c
	return c
}

func credOf(ctx plugin.Ctx) Cred { return sokel.CredentialAs[Cred](ctx) }

// gql runs one query and decodes data into out. GraphQL errors (HTTP 200 with an errors array) are errors too.
func gql(ctx context.Context, c Cred, query string, vars map[string]any, out any) error {
	tok := strings.TrimSpace(c.DeveloperToken)
	if tok == "" {
		return fmt.Errorf("凭证里没有 Developer token")
	}
	body, _ := json.Marshal(map[string]any{"query": query, "variables": vars})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := clientFor(c.Proxy).Do(req)
	if err != nil {
		return fmt.Errorf("连不上 Product Hunt（%v）。部署环境在国内时，到凭证里填「出站代理」", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	var env struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message          string `json:"message"`
			Error            string `json:"error"`
			ErrorDescription string `json:"error_description"`
		} `json:"errors"`
	}
	_ = json.Unmarshal(raw, &env)
	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("Developer token 无效（Product Hunt 回 401），到 api.producthunt.com/v2/oauth/applications 重新复制")
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return fmt.Errorf("Product Hunt 限流了（429）：调大轮询间隔")
	}
	if len(env.Errors) > 0 {
		var msgs []string
		for _, e := range env.Errors {
			msgs = append(msgs, strings.TrimSpace(e.Message+" "+e.Error+" "+e.ErrorDescription))
		}
		return fmt.Errorf("Product Hunt 返回错误：%s", strings.Join(msgs, "；"))
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("Product Hunt 回 HTTP %d：%s", resp.StatusCode, strings.TrimSpace(string(raw[:min(len(raw), 200)])))
	}
	if err := json.Unmarshal(env.Data, out); err != nil {
		return fmt.Errorf("Product Hunt 返回的不是预期的结构：%w", err)
	}
	return nil
}

var slugRe = regexp.MustCompile(`producthunt\.com/(?:posts|products)/([a-z0-9-]+)`)

// slugOf accepts a slug or a product page address.
func slugOf(s string) (string, error) {
	s = strings.TrimSpace(s)
	if m := slugRe.FindStringSubmatch(s); len(m) == 2 {
		return m[1], nil
	}
	if regexp.MustCompile(`^[a-z0-9-]+$`).MatchString(s) {
		return s, nil
	}
	return "", fmt.Errorf("认不出「%s」：填产品的 slug，或 producthunt.com/posts/… 地址", s)
}

func slugList(s string) []string {
	var out []string
	seen := map[string]bool{}
	for _, part := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == '，' || r == ' ' || r == '\n' }) {
		if sl, err := slugOf(part); err == nil && !seen[sl] {
			seen[sl] = true
			out = append(out, sl)
		}
	}
	return out
}

// —— response shapes (Product Hunt's field names) ——

type phUser struct {
	Username string `json:"username"`
	IsViewer bool   `json:"isViewer"`
}

// redacted reports the placeholder Product Hunt returns instead of another user's identity.
func (u phUser) redacted() bool { return u.Username == "" || u.Username == "[REDACTED]" }

type phComment struct {
	ID         string `json:"id"`
	Body       string `json:"body"`
	URL        string `json:"url"`
	VotesCount int    `json:"votesCount"`
	ParentID   string `json:"parentId"`
	CreatedAt  string `json:"createdAt"`
	User       phUser `json:"user"`
	Replies    struct {
		Edges []struct {
			Node phComment `json:"node"`
		} `json:"edges"`
	} `json:"replies"`
}

type phPageInfo struct {
	EndCursor   string `json:"endCursor"`
	HasNextPage bool   `json:"hasNextPage"`
}

type phPost struct {
	ID            string `json:"id"`
	Slug          string `json:"slug"`
	Name          string `json:"name"`
	Tagline       string `json:"tagline"`
	Description   string `json:"description"`
	URL           string `json:"url"`
	Website       string `json:"website"`
	VotesCount    int    `json:"votesCount"`
	CommentsCount int    `json:"commentsCount"`
	CreatedAt     string `json:"createdAt"`
	FeaturedAt    string `json:"featuredAt"`
	Makers        []struct {
		Username string `json:"username"`
	} `json:"makers"`
	Comments struct {
		TotalCount int        `json:"totalCount"`
		PageInfo   phPageInfo `json:"pageInfo"`
		Edges      []struct {
			Node phComment `json:"node"`
		} `json:"edges"`
	} `json:"comments"`
}
