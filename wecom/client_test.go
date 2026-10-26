package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Protocol fixtures below follow the official API examples, not a live tenant
// capture. These tests do not replace acceptance testing with a real WeCom app.
// Sources: document/path/90236 (send), /91023 (identity), /91039 (token).
const documentedTokenResponse = `{"errcode":0,"errmsg":"ok","access_token":"ACCESS_TOKEN","expires_in":7200}`
const documentedDeliveryResponse = `{"errcode":0,"errmsg":"ok","invaliduser":"userid1|userid2","invalidparty":"partyid1|partyid2","invalidtag":"tagid1|tagid2","unlicenseduser":"userid3|userid4","msgid":"xxxx","response_code":"xyzxyz"}`

func clientTestCred() Cred {
	return Cred{CorpID: "ww-test-corp", AgentID: "1000002", Secret: "TEST-SECRET"}
}

func newTestWeComClient(t *testing.T, handler http.HandlerFunc) *WeComClient {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	c := NewWeComClient()
	c.baseURL = srv.URL
	return c
}

func TestWeComMessageWireFormatAndPartialDelivery(t *testing.T) {
	var tokenCalls int
	var messages [][]byte
	c := newTestWeComClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cgi-bin/gettoken":
			tokenCalls++
			if r.Method != http.MethodGet || r.URL.Query().Get("corpid") != "ww-test-corp" || r.URL.Query().Get("corpsecret") != "TEST-SECRET" {
				t.Error("token request did not use the expected query and method")
			}
			io.WriteString(w, documentedTokenResponse)
		case "/cgi-bin/message/send":
			if r.Method != http.MethodPost || r.URL.Query().Get("access_token") != "ACCESS_TOKEN" {
				t.Error("send request did not use POST and the fetched access_token")
			}
			if got := r.Header.Get("Content-Type"); got != "application/json; charset=utf-8" {
				t.Errorf("unexpected content type %q", got)
			}
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Error(err)
			}
			messages = append(messages, body)
			io.WriteString(w, documentedDeliveryResponse)
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	users := []string{"alice", "bob"}
	delivery, err := c.SendText(context.Background(), clientTestCred(), users, "CPU > 95%\n请处理")
	if err != nil {
		t.Fatal(err)
	}
	wantDelivery := Delivery{MsgID: "xxxx", InvalidUser: "userid1|userid2", InvalidParty: "partyid1|partyid2", InvalidTag: "tagid1|tagid2", UnlicensedUser: "userid3|userid4"}
	if delivery != wantDelivery {
		t.Fatalf("partial delivery was lost: %#v", delivery)
	}
	const description = `<div class="normal">CPU &gt; 95% &amp; 内存不足</div>`
	_, err = c.SendCard(context.Background(), clientTestCred(), users, "告警", description, "https://alerts.example.com/a/1?x=1&y=2", "处理告警")
	if err != nil {
		t.Fatal(err)
	}
	if tokenCalls != 1 || len(messages) != 2 {
		t.Fatalf("token calls=%d messages=%d", tokenCalls, len(messages))
	}
	// Inspect the bytes actually received by HTTP, using independent shapes.
	var text struct {
		ToUser  string `json:"touser"`
		MsgType string `json:"msgtype"`
		AgentID int64  `json:"agentid"`
		Text    struct {
			Content string `json:"content"`
		} `json:"text"`
		TextCard json.RawMessage `json:"textcard"`
	}
	if err := json.Unmarshal(messages[0], &text); err != nil {
		t.Fatal(err)
	}
	if text.ToUser != "alice|bob" || text.MsgType != "text" || text.AgentID != 1000002 || text.Text.Content != "CPU > 95%\n请处理" || len(text.TextCard) != 0 {
		t.Fatalf("incorrect text wire payload: %s", messages[0])
	}
	var card struct {
		ToUser   string          `json:"touser"`
		MsgType  string          `json:"msgtype"`
		AgentID  int64           `json:"agentid"`
		Text     json.RawMessage `json:"text"`
		TextCard struct {
			Title       string `json:"title"`
			Description string `json:"description"`
			URL         string `json:"url"`
			Button      string `json:"btntxt"`
		} `json:"textcard"`
	}
	if err := json.Unmarshal(messages[1], &card); err != nil {
		t.Fatal(err)
	}
	if card.ToUser != "alice|bob" || card.MsgType != "textcard" || card.AgentID != 1000002 || len(card.Text) != 0 || card.TextCard.Title != "告警" || card.TextCard.Description != description || card.TextCard.URL != "https://alerts.example.com/a/1?x=1&y=2" || card.TextCard.Button != "处理告警" {
		t.Fatalf("incorrect card wire payload: %s", messages[1])
	}
}

func TestWeComTokenResponseFailures(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
	}{
		{"HTTP unauthorized", 401, "TEST-SECRET"},
		{"HTTP bad gateway", 502, "<html>TEST-SECRET</html>"},
		{"business error", 200, `{"errcode":60020,"errmsg":"corpsecret=TEST-SECRET"}`},
		{"invalid JSON", 200, "{TEST-SECRET"},
		{"missing errcode", 200, `{"access_token":"TEST-SECRET","expires_in":7200}`},
		{"null errcode", 200, `{"errcode":null,"access_token":"TEST-SECRET","expires_in":7200}`},
		{"null response", 200, "null"},
		{"wrong errcode type", 200, `{"errcode":"0","access_token":"TEST-SECRET","expires_in":7200}`},
		{"missing token", 200, `{"errcode":0,"expires_in":7200}`},
		{"empty token", 200, `{"errcode":0,"access_token":" ","expires_in":7200}`},
		{"wrong token type", 200, `{"errcode":0,"access_token":13,"expires_in":7200}`},
		{"missing expiry", 200, `{"errcode":0,"access_token":"TEST-SECRET"}`},
		{"zero expiry", 200, `{"errcode":0,"access_token":"TEST-SECRET","expires_in":0}`},
		{"negative expiry", 200, `{"errcode":0,"access_token":"TEST-SECRET","expires_in":-5}`},
		{"overflow expiry", 200, `{"errcode":0,"access_token":"TEST-SECRET","expires_in":9223372036854775807}`},
		{"trailing data", 200, documentedTokenResponse + `{}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestWeComClient(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				io.WriteString(w, tc.body)
			})
			_, err := c.AccessToken(context.Background(), clientTestCred())
			if err == nil {
				t.Fatal("expected failure")
			}
			if strings.Contains(err.Error(), "TEST-SECRET") {
				t.Fatal("upstream response leaked a credential")
			}
		})
	}
}

func TestWeComSendRejectsMalformedSuccess(t *testing.T) {
	for name, body := range map[string]string{
		"missing errcode": `{"msgid":"msg-1"}`,
		"missing msgid":   `{"errcode":0,"errmsg":"ok"}`,
		"empty msgid":     `{"errcode":0,"msgid":" "}`,
		"wrong msgid":     `{"errcode":0,"msgid":1}`,
		"wrong invalid":   `{"errcode":0,"msgid":"msg-1","invaliduser":["alice"]}`,
		"business error":  `{"errcode":81013,"errmsg":"no permission"}`,
	} {
		t.Run(name, func(t *testing.T) {
			c := newTestWeComClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/cgi-bin/gettoken" {
					io.WriteString(w, documentedTokenResponse)
					return
				}
				io.WriteString(w, body)
			})
			if _, err := c.SendText(context.Background(), clientTestCred(), []string{"alice"}, "告警"); err == nil {
				t.Fatal("malformed or failed delivery was accepted")
			}
		})
	}
}

func TestWeComTokenFailureRetriesOnlyOnce(t *testing.T) {
	for _, tc := range []struct {
		name       string
		status     int
		body       string
		alwaysFail bool
	}{
		{"HTTP 401", 401, "unauthorized", false},
		{"40001", 200, `{"errcode":40001,"errmsg":"invalid credential"}`, false},
		{"40014", 200, `{"errcode":40014,"errmsg":"invalid access_token"}`, false},
		{"42001", 200, `{"errcode":42001,"errmsg":"access_token expired"}`, false},
		{"persistent 401", 401, "unauthorized", true},
		{"persistent 40014", 200, `{"errcode":40014}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var tokens, sends int
			var payloads []string
			c := newTestWeComClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/cgi-bin/gettoken" {
					tokens++
					fmt.Fprintf(w, `{"errcode":0,"access_token":"token-%d","expires_in":7200}`, tokens)
					return
				}
				sends++
				b, _ := io.ReadAll(r.Body)
				payloads = append(payloads, string(b))
				if got := r.URL.Query().Get("access_token"); got != fmt.Sprintf("token-%d", sends) {
					t.Errorf("request %d used stale token %q", sends, got)
				}
				if sends == 1 || tc.alwaysFail {
					w.WriteHeader(tc.status)
					io.WriteString(w, tc.body)
					return
				}
				io.WriteString(w, `{"errcode":0,"msgid":"retry-success"}`)
			})
			delivery, err := c.SendText(context.Background(), clientTestCred(), []string{"alice"}, "告警")
			if (err != nil) != tc.alwaysFail {
				t.Fatalf("unexpected result: %#v %v", delivery, err)
			}
			if tokens != 2 || sends != 2 || payloads[0] != payloads[1] {
				t.Fatalf("expected one refresh and unchanged resend; tokens=%d sends=%d", tokens, sends)
			}
		})
	}
}

func TestWeComNoRetryOnAmbiguousOrUnrelatedFailure(t *testing.T) {
	for _, status := range []int{http.StatusBadGateway, http.StatusTooManyRequests, http.StatusOK} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var sends int
			c := newTestWeComClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/cgi-bin/gettoken" {
					io.WriteString(w, documentedTokenResponse)
					return
				}
				sends++
				w.WriteHeader(status)
				io.WriteString(w, `{"errcode":45009,"errmsg":"rate limit"}`)
			})
			if _, err := c.SendText(context.Background(), clientTestCred(), []string{"alice"}, "告警"); err == nil {
				t.Fatal("expected failure")
			}
			if sends != 1 {
				t.Fatalf("unsafe automatic retry: %d sends", sends)
			}
		})
	}
}

func TestWeComCredentialCacheIsolation(t *testing.T) {
	var calls int
	c := newTestWeComClient(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		fmt.Fprintf(w, `{"errcode":0,"access_token":"token-%d","expires_in":7200}`, calls)
	})
	a := clientTestCred()
	b, d, e := a, a, a
	b.Secret = "ROTATED-SECRET"
	d.AgentID = "1000003"
	e.CorpID = "another-corp"
	for i, cred := range []Cred{a, b, d, e, a, b} {
		token, err := c.AccessToken(context.Background(), cred)
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"token-1", "token-2", "token-3", "token-4", "token-1", "token-2"}[i]
		if token != want {
			t.Fatalf("cache crossed credential identity: got %q want %q", token, want)
		}
	}
	if calls != 4 {
		t.Fatalf("expected 4 independent credentials, got %d refreshes", calls)
	}
}

func TestWeComTokenConcurrentRefreshAndWaiterCancellation(t *testing.T) {
	var calls atomic.Int32
	started, release := make(chan struct{}), make(chan struct{})
	c := newTestWeComClient(t, func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		io.WriteString(w, documentedTokenResponse)
	})
	var wg sync.WaitGroup
	results := make(chan error, 32)
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			token, err := c.AccessToken(context.Background(), clientTestCred())
			if err == nil && token != "ACCESS_TOKEN" {
				err = fmt.Errorf("incorrect shared token")
			}
			results <- err
		}()
	}
	<-started
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.AccessToken(ctx, clientTestCred()); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled waiter got %v", err)
	}
	close(release)
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("concurrent callers performed %d refreshes", calls.Load())
	}
}

func TestWeComTokenExpiresEarlyAndLateErrorPreservesReplacement(t *testing.T) {
	var calls int
	c := newTestWeComClient(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		fmt.Fprintf(w, `{"errcode":0,"access_token":"token-%d","expires_in":100}`, calls)
	})
	now := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	c.now = func() time.Time { return now }
	first, err := c.AccessToken(context.Background(), clientTestCred())
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(89 * time.Second)
	if _, err := c.AccessToken(context.Background(), clientTestCred()); err != nil || calls != 1 {
		t.Fatalf("token refreshed prematurely: %d %v", calls, err)
	}
	now = now.Add(2 * time.Second)
	second, err := c.AccessToken(context.Background(), clientTestCred())
	if err != nil || second == first || calls != 2 {
		t.Fatalf("token did not refresh before expiry: %d %v", calls, err)
	}
	c.invalidateToken(clientTestCred(), first)
	third, err := c.AccessToken(context.Background(), clientTestCred())
	if err != nil || third != second || calls != 2 {
		t.Fatal("a late rejection evicted an already refreshed token")
	}
}

func TestWeComOAuthIdentityRequiresMember(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want string
	}{
		{"current documented response", `{"errcode":0,"errmsg":"ok","userid":"alice","user_ticket":"USER_TICKET"}`, "alice"},
		{"legacy UserId casing", `{"errcode":0,"UserId":"alice"}`, "alice"},
		{"nonmember", `{"errcode":0,"errmsg":"ok","openid":"OPENID","external_userid":"EXTERNAL_USERID"}`, ""},
		{"missing identity", `{"errcode":0}`, ""},
		{"invalid code", `{"errcode":40029,"errmsg":"invalid code"}`, ""},
		{"wrong identity type", `{"errcode":0,"userid":123}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestWeComClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/cgi-bin/gettoken" {
					io.WriteString(w, documentedTokenResponse)
					return
				}
				if r.URL.Path != "/cgi-bin/auth/getuserinfo" || r.Method != http.MethodGet || r.URL.Query().Get("code") != "AUTH-CODE+/=" || r.URL.Query().Get("access_token") != "ACCESS_TOKEN" {
					t.Error("identity call did not match the current official endpoint and query")
				}
				io.WriteString(w, tc.body)
			})
			got, err := c.UserID(context.Background(), clientTestCred(), "AUTH-CODE+/=")
			if tc.want == "" {
				if err == nil || got != "" {
					t.Fatalf("nonmember identity accepted: %q %v", got, err)
				}
			} else if err != nil || got != tc.want {
				t.Fatalf("got %q %v, want %q", got, err, tc.want)
			}
		})
	}
}

type clientRoundTripFunc func(*http.Request) (*http.Response, error)

func (f clientRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestWeComTransportErrorsDoNotLeakSecrets(t *testing.T) {
	c := NewWeComClient()
	c.httpClient.Transport = clientRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		return nil, &url.Error{Op: "Get", URL: r.URL.String(), Err: errors.New("TEST-SECRET ACCESS_TOKEN AUTH-CODE")}
	})
	for _, query := range []url.Values{
		{"corpsecret": {"TEST-SECRET"}},
		{"access_token": {"ACCESS_TOKEN"}, "code": {"AUTH-CODE"}},
	} {
		_, err := c.request(context.Background(), http.MethodGet, "/cgi-bin/example", query, nil)
		if err == nil {
			t.Fatal("expected transport failure")
		}
		for _, secret := range []string{"TEST-SECRET", "ACCESS_TOKEN", "AUTH-CODE", "http"} {
			if strings.Contains(err.Error(), secret) {
				t.Fatalf("transport error exposed sensitive details: %s", err)
			}
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.request(ctx, http.MethodGet, "/cgi-bin/example", nil, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("lost context cancellation: %v", err)
	}
}

func TestWeComDoesNotFollowRedirects(t *testing.T) {
	var redirected atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		redirected.Add(1)
		io.WriteString(w, documentedTokenResponse)
	}))
	t.Cleanup(target.Close)
	c := newTestWeComClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"?corpsecret=TEST-SECRET", http.StatusFound)
	})
	if _, err := c.AccessToken(context.Background(), clientTestCred()); err == nil {
		t.Fatal("redirect was accepted")
	}
	if redirected.Load() != 0 {
		t.Fatal("followed redirect to a different origin")
	}
	if c.httpClient.Timeout != 8*time.Second {
		t.Fatalf("unexpected timeout %s", c.httpClient.Timeout)
	}
}

func TestWeComInputValidationBeforeNetwork(t *testing.T) {
	c := NewWeComClient()
	c.httpClient.Transport = clientRoundTripFunc(func(_ *http.Request) (*http.Response, error) {
		t.Error("invalid input reached the network")
		return nil, errors.New("unexpected request")
	})
	for _, users := range [][]string{nil, {""}, {"alice|bob"}, {"@all"}, {" alice"}, {"a\nb"}, {"_alice"}, {"用户"}, {strings.Repeat("a", 65)}, make([]string, 1001)} {
		if _, err := c.SendText(context.Background(), clientTestCred(), users, "告警"); err == nil {
			t.Errorf("accepted invalid recipients %#v", users)
		}
	}
	for _, text := range []string{"", " ", strings.Repeat("中", 683), string([]byte{0xff})} {
		if _, err := c.SendText(context.Background(), clientTestCred(), []string{"alice"}, text); err == nil {
			t.Error("accepted invalid text")
		}
	}
	for _, cred := range []Cred{{}, {CorpID: "corp", AgentID: "NaN", Secret: "secret"}, {CorpID: "corp", AgentID: "-1", Secret: "secret"}} {
		if _, err := c.AccessToken(context.Background(), cred); err == nil {
			t.Error("accepted invalid credentials")
		}
	}
	for _, code := range []string{"", strings.Repeat("c", 513)} {
		if _, err := c.UserID(context.Background(), clientTestCred(), code); err == nil {
			t.Error("accepted invalid OAuth code")
		}
	}
	for _, tc := range [][4]string{
		{"", "body", "https://example.com", "处理"},
		{strings.Repeat("中", 129), "body", "https://example.com", "处理"},
		{"告警", strings.Repeat("中", 513), "https://example.com", "处理"},
		{"告警", "body", "javascript:alert(1)", "处理"},
		{"告警", "body", "https://user:password@example.com", "处理"},
		{"告警", "body", "https://example.com", "按钮超过四字"},
	} {
		if _, err := c.SendCard(context.Background(), clientTestCred(), []string{"alice"}, tc[0], tc[1], tc[2], tc[3]); err == nil {
			t.Errorf("accepted invalid card %#v", tc)
		}
	}
}

func TestWeComRecipientSupportsDocumentedIDCharacters(t *testing.T) {
	users := []string{"Alice@example.com", "user_1-2.3", strings.Repeat("a", 64)}
	got, err := weComRecipients(users)
	if err != nil || got != strings.Join(users, "|") {
		t.Fatalf("valid local member IDs were rejected: %q %v", got, err)
	}
}

func TestWeComDeliveryJSONUsesExplicitSnakeCase(t *testing.T) {
	b, err := json.Marshal(Delivery{MsgID: "m", InvalidUser: "u", InvalidParty: "p", InvalidTag: "t", UnlicensedUser: "l"})
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]string
	if err := json.Unmarshal(b, &fields); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"msg_id": "m", "invalid_user": "u", "invalid_party": "p", "invalid_tag": "t", "unlicensed_user": "l"}
	if !reflect.DeepEqual(fields, want) {
		t.Fatalf("delivery output fields changed: %s", b)
	}
}

func TestWeComVerifyApplicationRejectsFakeAndOtherApplicationSecrets(t *testing.T) {
	var tokenCalls, applicationCalls int
	c := newTestWeComClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cgi-bin/gettoken":
			tokenCalls++
			switch r.URL.Query().Get("corpsecret") {
			case "TEST-SECRET":
				io.WriteString(w, `{"errcode":0,"access_token":"right-application-token","expires_in":7200}`)
			case "OTHER-APPLICATION-SECRET":
				io.WriteString(w, `{"errcode":0,"access_token":"other-application-token","expires_in":7200}`)
			default:
				io.WriteString(w, `{"errcode":40001,"errmsg":"invalid credential"}`)
			}
		case "/cgi-bin/agent/get":
			applicationCalls++
			if r.Method != http.MethodGet || r.URL.Query().Get("agentid") != "1000002" {
				t.Error("application verification used the wrong method or AgentID")
			}
			if r.URL.Query().Get("access_token") == "right-application-token" {
				io.WriteString(w, `{"errcode":0,"errmsg":"ok","agentid":1000002,"name":"告警"}`)
			} else {
				io.WriteString(w, `{"errcode":48001,"errmsg":"api unauthorized"}`)
			}
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	cred := clientTestCred()
	if err := c.VerifyApplication(context.Background(), cred); err != nil {
		t.Fatal(err)
	}
	// The same claimed CorpID/AgentID must not inherit verification from a valid
	// credential when its Secret is fabricated or belongs to another application.
	for _, secret := range []string{"FAKE-SECRET", "OTHER-APPLICATION-SECRET"} {
		other := cred
		other.Secret = secret
		for i := 0; i < 2; i++ {
			if err := c.VerifyApplication(context.Background(), other); err == nil {
				t.Fatalf("unauthorized application access with secret %q", secret)
			}
		}
	}
	if tokenCalls != 4 || applicationCalls != 3 {
		t.Fatalf("failed checks were cached or credentials mixed: token calls=%d application calls=%d", tokenCalls, applicationCalls)
	}
	if err := c.VerifyApplication(context.Background(), cred); err != nil || applicationCalls != 3 {
		t.Fatalf("valid verification cache was unexpectedly changed: %v", err)
	}
}

func TestWeComVerifyApplicationRejectsMalformedOrMismatchedIdentity(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"different application", 200, `{"errcode":0,"agentid":1000003}`},
		{"missing application", 200, `{"errcode":0,"name":"告警"}`},
		{"null application", 200, `{"errcode":0,"agentid":null}`},
		{"wrong application type", 200, `{"errcode":0,"agentid":"1000002"}`},
		{"missing errcode", 200, `{"agentid":1000002}`},
		{"invalid JSON", 200, "{"},
		{"HTTP forbidden", 403, `{"errcode":0,"agentid":1000002}`},
		{"upstream rejection", 200, `{"errcode":48001,"agentid":1000002}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var checks int
			c := newTestWeComClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/cgi-bin/gettoken" {
					io.WriteString(w, documentedTokenResponse)
					return
				}
				checks++
				w.WriteHeader(tc.status)
				io.WriteString(w, tc.body)
			})
			for i := 0; i < 2; i++ {
				if err := c.VerifyApplication(context.Background(), clientTestCred()); err == nil {
					t.Fatal("invalid application ownership check succeeded")
				}
			}
			if checks != 2 {
				t.Fatal("a failed verification was cached")
			}
		})
	}
}

func TestWeComVerifyApplicationCacheExpiresWithinFiveMinutes(t *testing.T) {
	var checks int
	c := newTestWeComClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/cgi-bin/gettoken" {
			io.WriteString(w, documentedTokenResponse)
			return
		}
		checks++
		fmt.Fprintf(w, `{"errcode":0,"agentid":%s}`, r.URL.Query().Get("agentid"))
	})
	now := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	c.now = func() time.Time { return now }
	cred := clientTestCred()
	if err := c.VerifyApplication(context.Background(), cred); err != nil {
		t.Fatal(err)
	}
	now = now.Add(5*time.Minute - time.Second)
	if err := c.VerifyApplication(context.Background(), cred); err != nil || checks != 1 {
		t.Fatalf("valid verification was not cached: checks=%d err=%v", checks, err)
	}
	now = now.Add(time.Second)
	if err := c.VerifyApplication(context.Background(), cred); err != nil || checks != 2 {
		t.Fatalf("verification exceeded its five minute lifetime: checks=%d err=%v", checks, err)
	}
	other := cred
	other.AgentID = "1000003"
	if err := c.VerifyApplication(context.Background(), other); err != nil || checks != 3 {
		t.Fatalf("different AgentID reused verification: checks=%d err=%v", checks, err)
	}
	other = cred
	other.CorpID = "another-corp"
	if err := c.VerifyApplication(context.Background(), other); err != nil || checks != 4 {
		t.Fatalf("different CorpID reused verification: checks=%d err=%v", checks, err)
	}
}

func TestWeComVerifyApplicationHonorsContextDeadline(t *testing.T) {
	c := NewWeComClient()
	var checks atomic.Int32
	c.httpClient.Transport = clientRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		body := documentedTokenResponse
		if r.URL.Path == "/cgi-bin/agent/get" {
			if checks.Add(1) == 1 {
				<-r.Context().Done()
				return nil, r.Context().Err()
			}
			body = `{"errcode":0,"agentid":1000002}`
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := c.VerifyApplication(ctx, clientTestCred()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("verification lost context deadline: %v", err)
	}
	if err := c.VerifyApplication(context.Background(), clientTestCred()); err != nil || checks.Load() != 2 {
		t.Fatalf("timed out verification was cached: checks=%d err=%v", checks.Load(), err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	cancel()
	if err := c.VerifyApplication(ctx, clientTestCred()); !errors.Is(err, context.Canceled) {
		t.Fatalf("cached verification ignored cancellation: %v", err)
	}
}
