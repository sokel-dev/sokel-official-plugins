package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
)

type identityHandlerHost struct{ invoke map[string]plugin.Invoke }

func (h *identityHandlerHost) Register(op contract.Operation, fn plugin.Invoke) {
	h.invoke[op.ID] = fn
}

type identityHandlerSink struct{ value any }

func (s *identityHandlerSink) Vars(value any) { s.value = value }
func (*identityHandlerSink) Text(string)      {}
func (*identityHandlerSink) JSON(any)         {}

func TestIdentityApprovalHandlersThroughGeneratedContract(t *testing.T) {
	client := newTestWeComClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cgi-bin/gettoken":
			io.WriteString(w, documentedTokenResponse)
		case "/cgi-bin/agent/get":
			io.WriteString(w, `{"errcode":0,"agentid":1000002}`)
		case "/cgi-bin/get_jsapi_ticket", "/cgi-bin/ticket/get":
			io.WriteString(w, `{"errcode":0,"ticket":"SERVER_ONLY_TICKET","expires_in":7200}`)
		case "/cgi-bin/auth/getuserinfo":
			if r.URL.Query().Get("code") != "short-lived-code" {
				t.Error("handler lost code")
			}
			io.WriteString(w, `{"errcode":0,"userid":"WangXiaoMing"}`)
		case "/cgi-bin/oa/approval/create_template":
			io.WriteString(w, `{"errcode":0,"template_id":"new-template"}`)
		case "/cgi-bin/oa/approval/update_template":
			io.WriteString(w, `{"errcode":0}`)
		case "/cgi-bin/oa/applyevent":
			io.WriteString(w, `{"errcode":0,"sp_no":"202610260001"}`)
		case "/cgi-bin/oa/getapprovalinfo":
			var body struct {
				StartTime string `json:"starttime"`
				NewCursor string `json:"new_cursor"`
			}
			if json.NewDecoder(r.Body).Decode(&body) != nil || body.StartTime != "1760000000" || body.NewCursor != "cursor-1" {
				t.Error("generated JSON-body binding lost current pagination fields")
			}
			io.WriteString(w, `{"errcode":0,"sp_no_list":["202610260001"],"new_next_cursor":"cursor-2"}`)
		case "/cgi-bin/oa/getapprovaldetail":
			var body struct {
				SPNo string `json:"sp_no"`
			}
			if json.NewDecoder(r.Body).Decode(&body) != nil || body.SPNo != "202610260001" {
				t.Error("handler lost sp_no")
			}
			io.WriteString(w, `{"errcode":0,"info":{"sp_no":"202610260001","sp_status":2,"large_number":9007199254740993}}`)
		case "/cgi-bin/oa/gettemplatedetail":
			io.WriteString(w, `{"errcode":0,"template_names":[{"text":"模板","lang":"zh_CN"}],"template_content":{"controls":[]}}`)
		case "/cgi-bin/corp/getopenapprovaldata":
			var body struct {
				ThirdNo string `json:"thirdNo"`
			}
			if json.NewDecoder(r.Body).Decode(&body) != nil || body.ThirdNo != "business-1" {
				t.Error("handler lost snake-case third_no to upstream camel-case thirdNo mapping")
			}
			io.WriteString(w, `{"errcode":0,"data":{"ThirdNo":"business-1","OpenSpstatus":2}}`)
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
		}
	})
	a := &featureApplication{client: client, identity: NewWeComIdentityClient(client)}
	host := &identityHandlerHost{invoke: make(map[string]plugin.Invoke)}
	OnJsSdkConfig(host, a.jsSDKConfig)
	OnBuildWebLoginURL(host, a.buildWebLoginURL)
	OnExchangeLoginCode(host, a.exchangeLoginCode)
	OnApprovalTemplateGet(host, a.approvalTemplateGet)
	OnApprovalTemplateCreate(host, a.approvalTemplateCreate)
	OnApprovalTemplateUpdate(host, a.approvalTemplateUpdate)
	OnApprovalApply(host, a.approvalApply)
	OnApprovalGet(host, a.approvalGet)
	OnApprovalList(host, a.approvalList)
	OnOpenApprovalGet(host, a.openApprovalGet)
	ctx := &portalTestContext{Context: context.Background(), fields: map[string]string{"corp_id": "ww-test-corp", "agent_id": "1000002", "secret": "TEST-SECRET"}}
	cases := []struct {
		operation string
		input     string
		expected  string
	}{
		{"js_sdk_config", `{"page_url":"https://alerts.example.com/a?b=1#fragment"}`, `"signed_url":"https://alerts.example.com/a?b=1"`},
		{"build_web_login_url", `{"redirect_url":"https://alerts.example.com/login","state":"random-session-state","language":"en"}`, "login.work.weixin.qq.com/wwlogin/sso/login?"},
		{"exchange_login_code", `{"code":"short-lived-code"}`, `"user_id":"WangXiaoMing"`},
		{"approval_template_get", `{"template_id":"existing-template"}`, `"template_content":{"controls":[]}`},
		{"approval_template_create", `{"body":{"template_name":[{"text":"模板","lang":"zh_CN"}],"template_content":{"controls":[{"property":{"control":"Text","id":"Text-01"}}]}}}`, `"template_id":"new-template"`},
		{"approval_template_update", `{"body":{"template_id":"existing-template","template_name":[{"text":"模板","lang":"zh_CN"}],"template_content":{"controls":[{"property":{"control":"Text","id":"Text-01"}}]}}}`, `"ok":true`},
		{"approval_apply", `{"body":{"creator_userid":"WangXiaoMing","template_id":"existing-template","use_template_approver":1,"apply_data":{"contents":[]},"summary_list":[{"summary_info":[{"text":"告警处置","lang":"zh_CN"}]}]}}`, `"sp_no":"202610260001"`},
		{"approval_get", `{"sp_no":"202610260001"}`, `"large_number":9007199254740993`},
		{"approval_list", `{"body":{"starttime":"1760000000","endtime":"1760086400","new_cursor":"cursor-1"}}`, `"next_cursor":"cursor-2"`},
		{"open_approval_get", `{"third_no":"business-1"}`, `"ThirdNo":"business-1"`},
	}
	for _, tc := range cases {
		t.Run(tc.operation, func(t *testing.T) {
			sink := &identityHandlerSink{}
			if err := host.invoke[tc.operation](ctx, json.RawMessage(tc.input), sink); err != nil {
				t.Fatal(err)
			}
			b, err := json.Marshal(sink.value)
			if err != nil || !strings.Contains(string(b), tc.expected) {
				t.Fatalf("expected %s in bound output, got %s (%v)", tc.expected, b, err)
			}
			if strings.Contains(string(b), "SERVER_ONLY_TICKET") || strings.Contains(string(b), "TEST-SECRET") || strings.Contains(string(b), "ACCESS_TOKEN") {
				t.Error("handler leaked credentials")
			}
		})
	}
	ctx.fields["secret"] = ""
	for _, tc := range cases {
		if err := host.invoke[tc.operation](ctx, json.RawMessage(tc.input), &identityHandlerSink{}); err == nil {
			t.Errorf("%s accepted missing credentials", tc.operation)
		}
	}
}
