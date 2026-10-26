package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestWeComJSSignatureOfficialVector(t *testing.T) {
	// Verbatim inputs and expected digest from official document/path/90506.
	ticket := "sM4AOVdWfPE4DxkXGEs8VMCPGGVi4C3VM0P37wVUCFvkVAy_90u5h9nbSlYy3-Sl-HhTdfl2fzFy1AOcHKP7qg"
	got := weComJSSignature(ticket, "Wm3WZYTPz0wzccnW", 1414587457, "http://mp.weixin.qq.com?params=value")
	if got != "0f9de62fce790f9a083d5c99e95740ceb90c27ed" {
		t.Fatalf("official JS signature vector mismatch: %s", got)
	}
}

func TestWeComJSConfigCachesBothTicketsAndPreservesURL(t *testing.T) {
	var companyCalls, agentCalls atomic.Int32
	c := newTestWeComClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cgi-bin/gettoken":
			io.WriteString(w, documentedTokenResponse)
		case "/cgi-bin/agent/get":
			io.WriteString(w, `{"errcode":0,"agentid":1000002}`)
		case "/cgi-bin/get_jsapi_ticket":
			companyCalls.Add(1)
			io.WriteString(w, `{"errcode":0,"ticket":"COMPANY_TICKET","expires_in":7200}`)
		case "/cgi-bin/ticket/get":
			agentCalls.Add(1)
			if r.URL.Query().Get("type") != "agent_config" || r.URL.Query().Get("access_token") != "ACCESS_TOKEN" {
				t.Error("application ticket request omitted authenticated type=agent_config")
			}
			io.WriteString(w, `{"errcode":0,"ticket":"APPLICATION_TICKET","expires_in":7200}`)
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
		}
	})
	clock := time.Unix(1730000000, 0)
	c.now = func() time.Time { return clock }
	identity := NewWeComIdentityClient(c)
	pageURL := "https://alerts.example.com/a%2Fb?z=1&x=a%20b#client-fragment"
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, err := identity.JSConfig(context.Background(), clientTestCred(), pageURL)
			if err != nil {
				t.Error(err)
				return
			}
			wantURL := "https://alerts.example.com/a%2Fb?z=1&x=a%20b"
			if out.SignedURL != wantURL || out.Timestamp != clock.Unix() || out.AgentID != 1000002 || out.CorpID != "ww-test-corp" || len(out.NonceStr) != 32 {
				t.Errorf("invalid public signature metadata: %#v", out)
			}
			if out.Signature != weComJSSignature("COMPANY_TICKET", out.NonceStr, out.Timestamp, wantURL) || out.AgentSignature != weComJSSignature("APPLICATION_TICKET", out.NonceStr, out.Timestamp, wantURL) {
				t.Error("company/application ticket signatures crossed or URL was normalized")
			}
			b, _ := json.Marshal(out)
			if strings.Contains(string(b), "TICKET") || strings.Contains(string(b), "ACCESS_TOKEN") || strings.Contains(string(b), "TEST-SECRET") {
				t.Error("public config exposed a credential")
			}
		}()
	}
	wg.Wait()
	if companyCalls.Load() != 1 || agentCalls.Load() != 1 {
		t.Fatalf("concurrent cache miss caused excess ticket fetches: company=%d agent=%d", companyCalls.Load(), agentCalls.Load())
	}
	clock = clock.Add(7100 * time.Second) // Refresh ahead of the upstream expiry.
	if _, err := identity.JSConfig(context.Background(), clientTestCred(), pageURL); err != nil {
		t.Fatal(err)
	}
	other := clientTestCred()
	other.Secret = "ROTATED-SECRET"
	if _, err := identity.JSConfig(context.Background(), other, pageURL); err != nil {
		t.Fatal(err)
	}
	if companyCalls.Load() != 3 || agentCalls.Load() != 3 {
		t.Fatalf("ticket expiry/credential isolation failed: company=%d agent=%d", companyCalls.Load(), agentCalls.Load())
	}
}

func TestWeComJSTicketRejectsMalformedAndDoesNotCacheFailure(t *testing.T) {
	var calls atomic.Int32
	c := newTestWeComClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/cgi-bin/gettoken" {
			io.WriteString(w, documentedTokenResponse)
			return
		}
		if calls.Add(1) == 1 {
			io.WriteString(w, `{"errcode":0,"ticket":"SECRET_TICKET","expires_in":-1}`)
		} else {
			io.WriteString(w, `{"errcode":0,"ticket":"VALID_TICKET","expires_in":7200}`)
		}
	})
	identity := NewWeComIdentityClient(c)
	_, err := identity.jsTicket(context.Background(), clientTestCred(), false)
	if err == nil || strings.Contains(err.Error(), "SECRET_TICKET") {
		t.Fatalf("malformed ticket was accepted or leaked: %v", err)
	}
	ticket, err := identity.jsTicket(context.Background(), clientTestCred(), false)
	if err != nil || ticket != "VALID_TICKET" || calls.Load() != 2 {
		t.Fatalf("failed ticket response was cached: calls=%d error=%v", calls.Load(), err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := identity.jsTicket(ctx, clientTestCred(), false); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled caller used cache: %v", err)
	}
}

func TestWeComBuildCurrentWebLoginURL(t *testing.T) {
	state := "random-state-bound-to-session"
	redirect := "https://alerts.example.com/login?return=%2Fdashboard&x=1"
	raw, err := BuildWebLoginURL(clientTestCred(), redirect, state, "en")
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != "login.work.weixin.qq.com" || u.Path != "/wwlogin/sso/login" {
		t.Fatalf("not the current official Web login endpoint: %s", raw)
	}
	q := u.Query()
	if q.Get("login_type") != "CorpApp" || q.Get("appid") != "ww-test-corp" || q.Get("agentid") != "1000002" || q.Get("redirect_uri") != redirect || q.Get("state") != state || q.Get("lang") != "en" || strings.Contains(raw, "TEST-SECRET") {
		t.Fatalf("wrong login parameters: %s", raw)
	}
	for _, bad := range []string{"javascript:alert(1)", "https://user:pass@example.com", "http://example.com", "https://example.com/#callback", "https://example.com/\n"} {
		if _, err := BuildWebLoginURL(clientTestCred(), bad, state, "zh"); err == nil {
			t.Errorf("accepted invalid login redirect %q", bad)
		}
	}
	if _, err := BuildWebLoginURL(clientTestCred(), redirect, "", "zh"); err == nil {
		t.Error("allowed omitted CSRF state")
	}
	if _, err := BuildWebLoginURL(clientTestCred(), redirect, state, "xx"); err == nil {
		t.Error("allowed unsupported login language")
	}
}

func TestWeComApprovalWireFormatsAndPagination(t *testing.T) {
	// Upstream response examples/paths are from official /97437, /91853,
	// /91816, /90269. They are protocol fixtures, not real tenant submissions.
	var seen []string
	c := newTestWeComClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/cgi-bin/gettoken" {
			io.WriteString(w, documentedTokenResponse)
			return
		}
		if r.Method != http.MethodPost || r.URL.Query().Get("access_token") != "ACCESS_TOKEN" {
			t.Error("approval endpoint was not an authenticated POST")
		}
		var input map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Error(err)
		}
		seen = append(seen, r.URL.Path)
		switch r.URL.Path {
		case "/cgi-bin/oa/gettemplatedetail":
			if string(input["template_id"]) != `"TEMPLATE_ID"` {
				t.Error("missing template_id")
			}
			io.WriteString(w, `{"errcode":0,"template_names":[{"text":"模板","lang":"zh_CN"}],"template_content":{"controls":[]}}`)
		case "/cgi-bin/oa/approval/create_template":
			if !strings.Contains(string(input["template_content"]), "Text-01") {
				t.Error("template controls lost")
			}
			io.WriteString(w, `{"errcode":0,"template_id":"TEMPLATE_ID"}`)
		case "/cgi-bin/oa/approval/update_template":
			if string(input["template_id"]) != `"TEMPLATE_ID"` {
				t.Error("update template_id lost")
			}
			io.WriteString(w, `{"errcode":0,"errmsg":"ok"}`)
		case "/cgi-bin/oa/applyevent":
			if string(input["use_template_approver"]) != "1" || !strings.Contains(string(input["apply_data"]), "告警处理") {
				t.Error("approval process/form controls lost")
			}
			io.WriteString(w, `{"errcode":0,"errmsg":"ok","sp_no":"202001010001"}`)
		case "/cgi-bin/oa/getapprovaldetail":
			if string(input["sp_no"]) != `"202001010001"` {
				t.Error("sp_no missing")
			}
			io.WriteString(w, `{"errcode":0,"info":{"sp_no":"202001010001","sp_status":1}}`)
		case "/cgi-bin/oa/getapprovalinfo":
			if string(input["new_cursor"]) != `"opaque-next"` || string(input["size"]) != "100" || string(input["starttime"]) != `"1569546000"` {
				t.Error("new pagination/default size/string timestamps lost")
			}
			io.WriteString(w, `{"errcode":0,"errmsg":"ok","sp_no_list":["201909270001","201909270002"],"new_next_cursor":"opaque-last"}`)
		case "/cgi-bin/corp/getopenapprovaldata":
			if string(input["thirdNo"]) != `"thirdNoxxx"` || input["third_no"] != nil {
				t.Error("flow engine requires case-sensitive thirdNo")
			}
			io.WriteString(w, `{"errcode":0,"data":{"ThirdNo":"thirdNoxxx","OpenSpstatus":1}}`)
		default:
			t.Errorf("unexpected approval endpoint %s", r.URL.Path)
		}
	})
	ctx, cred := context.Background(), clientTestCred()
	if b, err := c.ApprovalTemplate(ctx, cred, "TEMPLATE_ID"); err != nil || !strings.Contains(string(b), "template_content") {
		t.Fatalf("get template: %s %v", b, err)
	}
	template := json.RawMessage(`{"template_name":[{"text":"模板","lang":"zh_CN"}],"template_content":{"controls":[{"property":{"control":"Text","id":"Text-01","title":[{"text":"名称","lang":"zh_CN"}]}}]}}`)
	if id, err := c.CreateApprovalTemplate(ctx, cred, template); err != nil || id != "TEMPLATE_ID" {
		t.Fatalf("create template: %q %v", id, err)
	}
	updated := json.RawMessage(strings.Replace(string(template), "{", `{"template_id":"TEMPLATE_ID",`, 1))
	if err := c.UpdateApprovalTemplate(ctx, cred, updated); err != nil {
		t.Fatal(err)
	}
	apply := json.RawMessage(`{"creator_userid":"WangXiaoMing","template_id":"TEMPLATE_ID","use_template_approver":1,"summary_list":[{"summary_info":[{"text":"告警处理","lang":"zh_CN"}]}],"apply_data":{"contents":[{"control":"Text","id":"Text-01","value":{"text":"告警处理"}}]}}`)
	if spNo, err := c.ApplyApproval(ctx, cred, apply); err != nil || spNo != "202001010001" {
		t.Fatalf("apply: %q %v", spNo, err)
	}
	if b, err := c.ApprovalDetail(ctx, cred, "202001010001"); err != nil || !strings.Contains(string(b), "sp_status") {
		t.Fatalf("get detail: %s %v", b, err)
	}
	page, err := c.ApprovalList(ctx, cred, json.RawMessage(`{"starttime":"1569546000","endtime":"1569718800","new_cursor":"opaque-next"}`))
	if err != nil || len(page.SPNoList) != 2 || page.NextCursor != "opaque-last" {
		t.Fatalf("list pagination: %#v %v", page, err)
	}
	if b, err := c.OpenApprovalData(ctx, cred, "thirdNoxxx"); err != nil || !strings.Contains(string(b), "OpenSpstatus") {
		t.Fatalf("flow engine: %s %v", b, err)
	}
	if len(seen) != 7 {
		t.Fatalf("expected seven distinct approval calls, got %v", seen)
	}
}

func TestWeComApprovalValidationAndPermissionError(t *testing.T) {
	var upstreamCalls atomic.Int32
	c := newTestWeComClient(t, func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		if r.URL.Path == "/cgi-bin/gettoken" {
			io.WriteString(w, documentedTokenResponse)
		} else {
			io.WriteString(w, `{"errcode":301088,"errmsg":"TEST-SECRET"}`)
		}
	})
	ctx, cred := context.Background(), clientTestCred()
	for _, invalid := range []string{"null", "[]", `{"starttime":"0","endtime":"2678401"}`, `{"starttime":"20","endtime":"10"}`, `{"starttime":"1","endtime":"10","size":101}`, `{"starttime":"1","endtime":"10","cursor":0}`, `{"starttime":1,"endtime":10}`} {
		if _, err := c.ApprovalList(ctx, cred, json.RawMessage(invalid)); err == nil {
			t.Errorf("accepted invalid list request: %s", invalid)
		}
	}
	for _, invalid := range []string{"null", "{}", `{"creator_userid":"member","template_id":"id","apply_data":{"contents":[]}}`, `{"creator_userid":"member","template_id":"id","use_template_approver":0,"apply_data":{"contents":[]}}`} {
		if _, err := c.ApplyApproval(ctx, cred, json.RawMessage(invalid)); err == nil {
			t.Errorf("accepted invalid submission: %s", invalid)
		}
	}
	if upstreamCalls.Load() != 0 {
		t.Fatalf("invalid input reached upstream: %d calls", upstreamCalls.Load())
	}
	_, err := c.ApprovalTemplate(ctx, cred, "TEMPLATE_ID")
	if err == nil || !strings.Contains(err.Error(), "可调用接口的应用") || strings.Contains(err.Error(), "TEST-SECRET") {
		t.Fatalf("approval permission failure was not actionable or leaked a secret: %v", err)
	}
}
