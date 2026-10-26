package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

// Request/response shapes follow official documents 90236, 94867, 90227/90228,
// 90231/90232/90233 and 92535. These HTTP fixtures are documented examples, not
// recordings of live tenant mutations; read-only acceptance is tested separately.
func TestAppFeaturesHTTPWire(t *testing.T) {
	type capture struct {
		method string
		path   string
		query  string
		body   json.RawMessage
	}
	var requests []capture
	c := newTestWeComClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/cgi-bin/gettoken" {
			io.WriteString(w, documentedTokenResponse)
			return
		}
		if r.URL.Query().Get("access_token") != "ACCESS_TOKEN" {
			t.Error("application operation omitted access token")
		}
		body, _ := io.ReadAll(r.Body)
		requests = append(requests, capture{r.Method, r.URL.Path, r.URL.Query().Get("agentid"), body})
		switch r.URL.Path {
		case "/cgi-bin/message/send":
			io.WriteString(w, documentedDeliveryResponse)
		case "/cgi-bin/agent/get":
			io.WriteString(w, `{"errcode":0,"errmsg":"ok","agentid":1000002,"name":"告警","allow_userinfos":{"user":[{"userid":"alice"}]}}`)
		case "/cgi-bin/menu/get":
			io.WriteString(w, `{"errcode":0,"errmsg":"ok","button":[{"name":"详情","type":"view","url":"https://example.com"}]}`)
		case "/cgi-bin/agent/get_workbench_template":
			io.WriteString(w, `{"errcode":0,"errmsg":"ok","type":"image","image":{"url":"https://example.com/banner.png"},"replace_user_data":true}`)
		case "/cgi-bin/agent/get_workbench_data":
			io.WriteString(w, `{"errcode":0,"errmsg":"ok","data":{"type":"keydata","keydata":{"items":[{"key":"待审批","data":"2"}]}}}`)
		default:
			io.WriteString(w, `{"errcode":0,"errmsg":"ok"}`)
		}
	})
	ctx, cred := context.Background(), clientTestCred()
	delivery, err := c.SendAppMessage(ctx, cred, []string{"alice", "bob"}, "template_card", json.RawMessage(`{"template_card":{"card_type":"button_interaction","main_title":{"title":"告警"},"button_list":[{"text":"确认","key":"confirm","style":1}]},"enable_duplicate_check":1,"duplicate_check_interval":1800}`))
	if err != nil {
		t.Fatal(err)
	}
	if delivery.MsgID != "xxxx" || delivery.ResponseCode != "xyzxyz" || delivery.InvalidUser != "userid1|userid2" || delivery.UnlicensedUser != "userid3|userid4" {
		t.Fatalf("delivery information lost: %+v", delivery)
	}
	if err := c.RecallMessage(ctx, cred, "msg-1"); err != nil {
		t.Fatal(err)
	}
	application, err := c.GetApplication(ctx, cred)
	if err != nil || !strings.Contains(string(application), `"allow_userinfos"`) {
		t.Fatalf("application response lost: %s %v", application, err)
	}
	if err := c.SetApplication(ctx, cred, json.RawMessage(`{"home_url":"https://example.com/alerts","isreportenter":0}`)); err != nil {
		t.Fatal(err)
	}
	menu, err := c.GetMenu(ctx, cred)
	if err != nil || !strings.Contains(string(menu), `"button"`) {
		t.Fatalf("menu response lost: %s %v", menu, err)
	}
	const menuJSON = `{"button":[{"type":"click","name":"待处理","key":"pending"},{"name":"更多","sub_button":[{"type":"view","name":"告警列表","url":"https://example.com/alerts"}]}]}`
	if err := c.SetMenu(ctx, cred, json.RawMessage(menuJSON)); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteMenu(ctx, cred); err != nil {
		t.Fatal(err)
	}
	template, err := c.GetWorkbenchTemplate(ctx, cred)
	if err != nil || !strings.Contains(string(template), `"replace_user_data":true`) {
		t.Fatalf("template response lost: %s %v", template, err)
	}
	if err := c.SetWorkbenchTemplate(ctx, cred, json.RawMessage(`{"type":"keydata","keydata":{"items":[{"key":"待处理","data":"2","jump_url":"https://example.com/alerts"}]},"replace_user_data":false}`)); err != nil {
		t.Fatal(err)
	}
	const dataJSON = `{"type":"list","list":{"items":[{"title":"CPU 告警","jump_url":"https://example.com/alerts/1"}]}}`
	if err := c.SetWorkbenchData(ctx, cred, "alice", json.RawMessage(dataJSON)); err != nil {
		t.Fatal(err)
	}
	data, err := c.GetWorkbenchData(ctx, cred, "alice")
	if err != nil || !strings.Contains(string(data), `"data":"2"`) {
		t.Fatalf("user data response lost: %s %v", data, err)
	}
	if err := c.BatchSetWorkbenchData(ctx, cred, []string{"alice", "bob"}, json.RawMessage(dataJSON)); err != nil {
		t.Fatal(err)
	}
	if len(requests) != 12 {
		t.Fatalf("requests=%d, want 12", len(requests))
	}
	want := []capture{
		{http.MethodPost, "/cgi-bin/message/send", "", json.RawMessage(`{"touser":"alice|bob","agentid":1000002,"msgtype":"template_card","template_card":{"card_type":"button_interaction","main_title":{"title":"告警"},"button_list":[{"text":"确认","key":"confirm","style":1}]},"enable_duplicate_check":1,"duplicate_check_interval":1800}`)},
		{http.MethodPost, "/cgi-bin/message/recall", "", json.RawMessage(`{"msgid":"msg-1"}`)},
		{http.MethodGet, "/cgi-bin/agent/get", "1000002", nil},
		{http.MethodPost, "/cgi-bin/agent/set", "", json.RawMessage(`{"agentid":1000002,"home_url":"https://example.com/alerts","isreportenter":0}`)},
		{http.MethodGet, "/cgi-bin/menu/get", "1000002", nil},
		{http.MethodPost, "/cgi-bin/menu/create", "1000002", json.RawMessage(menuJSON)},
		{http.MethodGet, "/cgi-bin/menu/delete", "1000002", nil},
		{http.MethodPost, "/cgi-bin/agent/get_workbench_template", "", json.RawMessage(`{"agentid":1000002}`)},
		{http.MethodPost, "/cgi-bin/agent/set_workbench_template", "", json.RawMessage(`{"agentid":1000002,"type":"keydata","keydata":{"items":[{"key":"待处理","data":"2","jump_url":"https://example.com/alerts"}]},"replace_user_data":false}`)},
		{http.MethodPost, "/cgi-bin/agent/set_workbench_data", "", json.RawMessage(`{"agentid":1000002,"userid":"alice","type":"list","list":{"items":[{"title":"CPU 告警","jump_url":"https://example.com/alerts/1"}]}}`)},
		{http.MethodPost, "/cgi-bin/agent/get_workbench_data", "", json.RawMessage(`{"agentid":1000002,"userid":"alice"}`)},
		{http.MethodPost, "/cgi-bin/agent/batch_set_workbench_data", "", json.RawMessage(`{"agentid":1000002,"userid_list":["alice","bob"],"data":` + dataJSON + `}`)},
	}
	for i, expected := range want {
		got := requests[i]
		if got.method != expected.method || got.path != expected.path || got.query != expected.query {
			t.Errorf("request %d routing: %+v, want %+v", i, got, expected)
		}
		if len(got.body) == 0 && len(expected.body) == 0 {
			continue
		}
		if compactJSONForTest(t, got.body) != compactJSONForTest(t, expected.body) {
			t.Errorf("request %d body=%s, want %s", i, got.body, expected.body)
		}
	}
}

func compactJSONForTest(t *testing.T, raw []byte) string {
	t.Helper()
	// Unmarshal recursively through a RawMessage map to compare JSON object order
	// while preserving exact integer values (never convert through float64).
	var decoded interface{}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		t.Fatal(err)
	}
	canonical, err := json.Marshal(decoded)
	if err != nil {
		t.Fatal(err)
	}
	return string(canonical)
}

func TestAppFeaturesRejectInvalidInputBeforeNetwork(t *testing.T) {
	c := newTestWeComClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("invalid input reached HTTP: %s", r.URL.Path)
		w.WriteHeader(http.StatusInternalServerError)
	})
	ctx, cred := context.Background(), clientTestCred()
	cases := []struct {
		name string
		call func() error
	}{
		{"message broad recipient", func() error {
			_, err := c.SendAppMessage(ctx, cred, []string{"@all"}, "text", json.RawMessage(`{"text":{"content":"test"}}`))
			return err
		}},
		{"message unsupported type", func() error {
			_, err := c.SendAppMessage(ctx, cred, []string{"alice"}, "event", json.RawMessage(`{"event":{}}`))
			return err
		}},
		{"message wrong nested shape", func() error {
			_, err := c.SendAppMessage(ctx, cred, []string{"alice"}, "text", json.RawMessage(`{"text":[]}`))
			return err
		}},
		{"message wrong nested key", func() error {
			_, err := c.SendAppMessage(ctx, cred, []string{"alice"}, "text", json.RawMessage(`{"image":{}}`))
			return err
		}},
		{"message invalid flag", func() error {
			_, err := c.SendAppMessage(ctx, cred, []string{"alice"}, "text", json.RawMessage(`{"text":{},"safe":2}`))
			return err
		}},
		{"message fractional flag", func() error {
			_, err := c.SendAppMessage(ctx, cred, []string{"alice"}, "text", json.RawMessage(`{"text":{},"enable_id_trans":0.5}`))
			return err
		}},
		{"message excessive dedupe", func() error {
			_, err := c.SendAppMessage(ctx, cred, []string{"alice"}, "text", json.RawMessage(`{"text":{},"duplicate_check_interval":14401}`))
			return err
		}},
		{"empty recall", func() error { return c.RecallMessage(ctx, cred, " ") }},
		{"empty application patch", func() error { return c.SetApplication(ctx, cred, json.RawMessage(`{}`)) }},
		{"application invalid URL", func() error {
			return c.SetApplication(ctx, cred, json.RawMessage(`{"home_url":"javascript:alert(1)"}`))
		}},
		{"application short description", func() error { return c.SetApplication(ctx, cred, json.RawMessage(`{"description":"abc"}`)) }},
		{"application invalid flag", func() error { return c.SetApplication(ctx, cred, json.RawMessage(`{"isreportenter":null}`)) }},
		{"application invalid domain", func() error {
			return c.SetApplication(ctx, cred, json.RawMessage(`{"redirect_domain":"https://example.com"}`))
		}},
		{"menu no button", func() error { return c.SetMenu(ctx, cred, json.RawMessage(`{}`)) }},
		{"menu empty button", func() error { return c.SetMenu(ctx, cred, json.RawMessage(`{"button":[]}`)) }},
		{"menu invalid URL", func() error {
			return c.SetMenu(ctx, cred, json.RawMessage(`{"button":[{"name":"入口","type":"view","url":"javascript:alert(1)"}]}`))
		}},
		{"menu missing event key", func() error {
			return c.SetMenu(ctx, cred, json.RawMessage(`{"button":[{"name":"入口","type":"click"}]}`))
		}},
		{"menu unsupported action", func() error {
			return c.SetMenu(ctx, cred, json.RawMessage(`{"button":[{"name":"入口","type":"unknown"}]}`))
		}},
		{"menu third level", func() error {
			return c.SetMenu(ctx, cred, json.RawMessage(`{"button":[{"name":"1","sub_button":[{"name":"2","sub_button":[{"name":"3","type":"click","key":"x"}]}]}]}`))
		}},
		{"menu empty mini app", func() error {
			return c.SetMenu(ctx, cred, json.RawMessage(`{"button":[{"name":"入口","type":"view_miniprogram","appid":"","pagepath":"pages/index"}]}`))
		}},
		{"workbench wrong type", func() error { return c.SetWorkbenchTemplate(ctx, cred, json.RawMessage(`{"type":"unknown"}`)) }},
		{"workbench wrong nested key", func() error { return c.SetWorkbenchTemplate(ctx, cred, json.RawMessage(`{"type":"image","list":{}}`)) }},
		{"workbench invalid replace", func() error {
			return c.SetWorkbenchTemplate(ctx, cred, json.RawMessage(`{"type":"normal","replace_user_data":1}`))
		}},
		{"workbench user data missing", func() error { return c.SetWorkbenchData(ctx, cred, "alice", json.RawMessage(`{"type":"image"}`)) }},
		{"workbench normal user data", func() error { return c.SetWorkbenchData(ctx, cred, "alice", json.RawMessage(`{"type":"normal"}`)) }},
		{"workbench invalid user", func() error {
			return c.SetWorkbenchData(ctx, cred, "alice|bob", json.RawMessage(`{"type":"image","image":{}}`))
		}},
		{"workbench invalid get user", func() error { _, err := c.GetWorkbenchData(ctx, cred, "@all"); return err }},
		{"workbench empty batch", func() error {
			return c.BatchSetWorkbenchData(ctx, cred, nil, json.RawMessage(`{"type":"image","image":{}}`))
		}},
	}
	for _, field := range []string{"agentid", "touser", "toparty", "totag", "msgtype", "AgentID", "access_token"} {
		field := field
		cases = append(cases, struct {
			name string
			call func() error
		}{"message reserved " + field, func() error {
			_, err := c.SendAppMessage(ctx, cred, []string{"alice"}, "text", json.RawMessage(fmt.Sprintf(`{"text":{"content":"test"},%q:"override"}`, field)))
			return err
		}})
	}
	for _, field := range []string{"agentid", "userid", "userid_list"} {
		field := field
		cases = append(cases, struct {
			name string
			call func() error
		}{"workbench reserved " + field, func() error {
			return c.SetWorkbenchData(ctx, cred, "alice", json.RawMessage(fmt.Sprintf(`{"type":"image","image":{"url":"https://example.com/image.png"},%q:"override"}`, field)))
		}})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); err == nil {
				t.Fatal("expected invalid input failure")
			}
		})
	}
}

func TestAppFeatureJSONObjectValidation(t *testing.T) {
	for _, input := range []string{"", "null", "[]", "3", `"string"`, "{} {}", `{"a":1,"a":2}`, `{"a":}`, `{"a":1`, string([]byte{'{', '"', 0xff, '"', ':', '1', '}'})} {
		t.Run(fmt.Sprintf("%q", input), func(t *testing.T) {
			if _, err := apiObject(json.RawMessage(input)); err == nil {
				t.Fatalf("accepted invalid object %q", input)
			}
		})
	}
	input := json.RawMessage(`{"nested":{"large_id":9007199254740993,"items":[{"a":true}]}}`)
	object, err := apiObject(input)
	if err != nil || !reflect.DeepEqual(object["nested"], json.RawMessage(`{"large_id":9007199254740993,"items":[{"a":true}]}`)) {
		t.Fatalf("nested structure altered: %s %v", object["nested"], err)
	}
}

func TestAppFeatureGetApplicationRejectsMismatch(t *testing.T) {
	for _, response := range []string{`{"errcode":0}`, `{"errcode":0,"agentid":1000003}`, `{"errcode":0,"agentid":"1000002"}`} {
		t.Run(response, func(t *testing.T) {
			c := newTestWeComClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/cgi-bin/gettoken" {
					io.WriteString(w, documentedTokenResponse)
				} else {
					io.WriteString(w, response)
				}
			})
			if _, err := c.GetApplication(context.Background(), clientTestCred()); err == nil {
				t.Fatal("accepted mismatched application identity")
			}
		})
	}
}

func TestAppFeatureMessageTypesAndNormalWorkbench(t *testing.T) {
	count := 0
	c := newTestWeComClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/cgi-bin/gettoken" {
			io.WriteString(w, documentedTokenResponse)
			return
		}
		count++
		if r.URL.Path == "/cgi-bin/message/send" {
			io.WriteString(w, documentedDeliveryResponse)
			return
		}
		io.WriteString(w, `{"errcode":0,"errmsg":"ok"}`)
	})
	for _, kind := range []string{"text", "image", "voice", "video", "file", "textcard", "news", "mpnews", "markdown", "miniprogram_notice", "template_card"} {
		// Field-level content validation belongs to the upstream API; here verify
		// routing permits each documented union variant without losing its name.
		body := fmt.Sprintf(`{%q:{}}`, kind)
		if kind == "mpnews" {
			body = `{"mpnews":{},"safe":2}`
		}
		if _, err := c.SendAppMessage(context.Background(), clientTestCred(), []string{"alice"}, kind, json.RawMessage(body)); err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
	}
	if err := c.SetWorkbenchTemplate(context.Background(), clientTestCred(), json.RawMessage(`{"type":"normal"}`)); err != nil {
		t.Fatal(err)
	}
	if count != 12 {
		t.Fatalf("calls=%d, want 12", count)
	}
}
