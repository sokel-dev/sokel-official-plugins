package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"testing"
)

func TestUpdateTemplateCardWireAndPartialUsers(t *testing.T) {
	var bodies [][]byte
	c := newTestWeComClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/cgi-bin/gettoken" {
			io.WriteString(w, documentedTokenResponse)
			return
		}
		if r.Method != http.MethodPost || r.URL.Path != "/cgi-bin/message/update_template_card" || r.URL.Query().Get("access_token") != "ACCESS_TOKEN" {
			t.Errorf("wrong update method/path: %s %s", r.Method, r.URL.Path)
		}
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, b)
		// Official /94888 success example includes invaliduser even when errcode=0.
		io.WriteString(w, `{"errcode":0,"errmsg":"ok","invaliduser":["userid1","userid2"]}`)
	})
	ctx, cred := context.Background(), clientTestCred()
	result, err := c.UpdateTemplateCard(ctx, cred, []string{"alice", "bob"}, "CODE_FROM_SEND", json.RawMessage(`{"button":{"replace_name":"已处理"}}`))
	if err != nil || !reflect.DeepEqual(result.InvalidUsers, []string{"userid1", "userid2"}) {
		t.Fatalf("partial failure lost: %+v %v", result, err)
	}
	const replacement = `{"template_card":{"card_type":"text_notice","main_title":{"title":"已确认"},"card_action":{"type":1,"url":"https://example.com/alerts/1"}},"enable_id_trans":1}`
	_, err = c.UpdateTemplateCard(ctx, cred, []string{"alice"}, "CODE_FROM_CALLBACK", json.RawMessage(replacement))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		`{"userids":["alice","bob"],"agentid":1000002,"response_code":"CODE_FROM_SEND","button":{"replace_name":"已处理"}}`,
		`{"userids":["alice"],"agentid":1000002,"response_code":"CODE_FROM_CALLBACK","template_card":{"card_type":"text_notice","main_title":{"title":"已确认"},"card_action":{"type":1,"url":"https://example.com/alerts/1"}},"enable_id_trans":1}`,
	}
	if len(bodies) != 2 {
		t.Fatalf("calls=%d, want 2", len(bodies))
	}
	for i := range bodies {
		if compactJSONForTest(t, bodies[i]) != compactJSONForTest(t, []byte(want[i])) {
			t.Errorf("body=%s, want %s", bodies[i], want[i])
		}
	}
}

func TestUpdateTemplateCardRejectsUnsafeInputs(t *testing.T) {
	c := newTestWeComClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("invalid input reached HTTP: %s", r.URL.Path)
		w.WriteHeader(500)
	})
	bodies := []string{
		`{}`, `null`, `[]`,
		`{"button":null}`,
		`{"button":{"replace_name":""}}`,
		`{"button":{"replace_name":"已处理"},"template_card":{"card_type":"text_notice"}}`,
		`{"button":{"replace_name":"已处理"},"enable_id_trans":1}`,
		`{"button":{"replace_name":"已处理","url":"https://example.com"}}`,
		`{"template_card":[]}`,
		`{"template_card":{}}`,
		`{"template_card":{"card_type":"unsupported"}}`,
		`{"template_card":{"card_type":"text_notice"},"enable_id_trans":true}`,
	}
	for _, field := range []string{"agentid", "userids", "partyids", "tagids", "atall", "response_code", "access_token"} {
		bodies = append(bodies, fmt.Sprintf(`{"button":{"replace_name":"已处理"},%q:"override"}`, field))
	}
	for _, body := range bodies {
		t.Run(body, func(t *testing.T) {
			if _, err := c.UpdateTemplateCard(context.Background(), clientTestCred(), []string{"alice"}, "CODE", json.RawMessage(body)); err == nil {
				t.Fatal("accepted invalid/unsafe update")
			}
		})
	}
	for _, users := range [][]string{nil, {"@all"}, make([]string, 1001)} {
		if _, err := c.UpdateTemplateCard(context.Background(), clientTestCred(), users, "CODE", json.RawMessage(`{"button":{"replace_name":"已处理"}}`)); err == nil {
			t.Fatal("accepted invalid update recipients")
		}
	}
	if _, err := c.UpdateTemplateCard(context.Background(), clientTestCred(), []string{"alice"}, "", json.RawMessage(`{"button":{"replace_name":"已处理"}}`)); err == nil {
		t.Fatal("accepted empty response code")
	}
}

func TestUpdateTemplateCardAllFiveVariants(t *testing.T) {
	c := newTestWeComClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/cgi-bin/gettoken" {
			io.WriteString(w, documentedTokenResponse)
		} else {
			io.WriteString(w, `{"errcode":0,"errmsg":"ok"}`)
		}
	})
	for _, kind := range []string{"text_notice", "news_notice", "button_interaction", "vote_interaction", "multiple_interaction"} {
		body := fmt.Sprintf(`{"template_card":{"card_type":%q}}`, kind)
		result, err := c.UpdateTemplateCard(context.Background(), clientTestCred(), []string{"alice"}, "CODE", json.RawMessage(body))
		if err != nil || result.InvalidUsers == nil || len(result.InvalidUsers) != 0 {
			t.Fatalf("%s: %+v %v", kind, result, err)
		}
	}
}
