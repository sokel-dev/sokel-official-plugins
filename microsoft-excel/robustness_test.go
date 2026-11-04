package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type testCtx struct {
	context.Context
	cred     map[string]string
	file     []byte
	fetchErr error
}

func (c *testCtx) Credential() map[string]string      { return c.cred }
func (c *testCtx) Fetch(*plugin.File) ([]byte, error) { return c.file, c.fetchErr }
func (c *testCtx) Upload(string, string, []byte) (*plugin.File, error) {
	return nil, errors.New("unused")
}
func (c *testCtx) UploadReader(string, string, io.Reader) (*plugin.File, error) {
	return nil, errors.New("unused")
}

type testHost struct{ calls map[string]plugin.Invoke }

func (h *testHost) Register(op contract.Operation, fn plugin.Invoke) { h.calls[op.ID] = fn }

type testSink struct{ vars map[string]any }

func (s *testSink) Vars(v any) { s.vars = contract.StructToVars(v) }
func (*testSink) Text(string)  {}
func (*testSink) JSON(any)     {}
func invoke(t *testing.T, c *client, op, body string, ctx *testCtx) map[string]any {
	t.Helper()
	old := http.DefaultTransport
	http.DefaultTransport = c.http.Transport
	defer func() { http.DefaultTransport = old }()
	h := &testHost{calls: map[string]plugin.Invoke{}}
	registerOperations(h)
	if ctx == nil {
		ctx = &testCtx{Context: context.Background()}
	}
	ctx.cred = map[string]string{"access_token": "test-access-token"}
	sink := &testSink{}
	if e := h.calls[op](ctx, json.RawMessage(body), sink); e != nil {
		t.Fatal(e)
	}
	return sink.vars
}
func TestGeneratedHealth(t *testing.T) {
	status := 200
	body := `{"id":"user-1"}`
	c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1.0/me" || r.URL.Query().Get("$select") != "id" {
			t.Fatal("health was not /me")
		}
		w.WriteHeader(status)
		reply(w, body)
	})
	if v := invoke(t, c, "health_check", `{}`, nil); v["ok"] != true {
		t.Fatal(v)
	}
	status = 401
	if v := invoke(t, c, "health_check", `{}`, nil); v["ok"] != false || v["message"] == "" {
		t.Fatal(v)
	}
	status = 200
	body = `{}`
	if v := invoke(t, c, "health_check", `{}`, nil); v["ok"] != false {
		t.Fatal("fake success")
	}
	p := sokel.New(sokel.Config{Name: "test-plugin"})
	register(p)
}
func TestErrorsDoNotLeakOrRetry(t *testing.T) {
	for _, status := range []int{400, 401, 403, 404, 409, 410, 412, 429, 500, 202} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var calls atomic.Int32
			c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Retry-After", "90")
				w.WriteHeader(status)
				reply(w, `{"error":"test-access-token and private signed URL"}`)
			})
			var out map[string]any
			e := c.json(context.Background(), "POST", graphOrigin+"/v1.0/probe", map[string]any{"value": false}, nil, &out, 200)
			if e == nil || strings.Contains(e.Error(), "test-access-token") || calls.Load() != 1 {
				t.Fatalf("unsafe/retried error %v", e)
			}
			var ae *apiError
			if !errors.As(e, &ae) || ae.Status != status {
				t.Fatal(e)
			}
			if status == 429 && ae.RetryAfter != 90*time.Second {
				t.Fatal("lost retry-after")
			}
		})
	}
}
func TestRedirectJSONAndTransportBoundaries(t *testing.T) {
	var leaked atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked.Add(1) }))
	defer target.Close()
	c := fixture(t, func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 302) })
	var out map[string]any
	if e := c.json(context.Background(), "GET", graphOrigin+"/v1.0/probe", nil, nil, &out, 200); e == nil || leaked.Load() != 0 {
		t.Fatal("redirect followed")
	}
	for _, body := range []string{`not JSON`, `{} {}`, strings.Repeat("x", maxJSON+1)} {
		c = fixture(t, func(w http.ResponseWriter, r *http.Request) { reply(w, body) })
		if e := c.json(context.Background(), "GET", graphOrigin+"/v1.0/probe", nil, nil, &out, 200); e == nil {
			t.Fatal("invalid response accepted")
		}
	}
	c.http = &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("test-access-token in transport error")
	})}
	if e := c.json(context.Background(), "GET", graphOrigin+"/v1.0/probe", nil, nil, &out, 200); e == nil || strings.Contains(e.Error(), "test-access-token") {
		t.Fatal("transport leak")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if e := c.json(ctx, "GET", graphOrigin+"/v1.0/probe", nil, nil, &out, 200); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	if e := c.json(context.Background(), "POST", graphOrigin+"/v1.0/probe", map[string]any{"bad": make(chan int)}, nil, &out, 200); e == nil {
		t.Fatal("invalid JSON input")
	}
	if e := c.json(context.Background(), "POST", graphOrigin+"/v1.0/probe", map[string]string{"large": strings.Repeat("x", 1<<20)}, nil, &out, 200); e == nil {
		t.Fatal("oversized JSON input")
	}
}
func TestContinuationOriginAndPath(t *testing.T) {
	path := "/v1.0/drives/drive/root/children"
	for _, bad := range []string{"http://graph.microsoft.com" + path, "https://graph.microsoft.com.evil.test" + path, "https://graph.microsoft.com:443" + path, "https://u:p@graph.microsoft.com" + path, "https://graph.microsoft.com/beta/drives/drive/root/children", "https://graph.microsoft.com/v1.0/me/messages", "https://graph.microsoft.com" + path + "#fragment", "https://graph.microsoft.com/v1.0/drives/drive/root/../children"} {
		if _, e := pageURL(path, bad, nil); e == nil {
			t.Fatalf("unsafe continuation accepted: %s", bad)
		}
	}
	good := graphOrigin + path + "?$skiptoken=a%2Bb%2F%3D"
	if got, e := pageURL(path, good, nil); e != nil || got != good {
		t.Fatal("cursor changed")
	}
	if _, e := checkedNext(path, good, good); e == nil {
		t.Fatal("repeated cursor")
	}
	for _, bad := range []string{"../drive", "a/b", "a%2fb", "a?x=1", "", ".."} {
		if _, e := segment(bad); e == nil {
			t.Fatal("invalid resource ID")
		}
	}
	if retryAfter("garbage") != time.Minute || retryAfter("0") != time.Minute || retryAfter(time.Now().Add(2*time.Minute).UTC().Format(http.TimeFormat)) < 119*time.Second {
		t.Fatal("retry-after parse")
	}
}
func TestCredentialAndOriginBeforeNetwork(t *testing.T) {
	c := newClient(Cred{AccessToken: "test"})
	c.http = &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
		t.Fatal("invalid input reached transport")
		return nil, nil
	})}
	if _, _, e := c.send(context.Background(), "GET", "https://evil.test/v1.0/me", nil, nil, 200); e == nil {
		t.Fatal("foreign origin")
	}
	for _, token := range []string{"", "x\r\nAuthorization: other", "a b"} {
		c.cred.AccessToken = token
		if _, _, e := c.send(context.Background(), "GET", graphOrigin+"/v1.0/me", nil, nil, 200); e == nil {
			t.Fatal("invalid token")
		}
	}
	c.cred.AccessToken = "test"
	if _, _, e := c.send(context.Background(), "POST", graphOrigin+"/v1.0/me", nil, http.Header{"Workbook-Session-Id": {"bad\r\nvalue"}}, 200); e == nil {
		t.Fatal("header injection")
	}
	if _, _, e := c.send(context.Background(), "PUT", graphOrigin+"/v1.0/me", make([]byte, (4<<20)+1), nil, 200); e == nil {
		t.Fatal("oversized file")
	}
}
