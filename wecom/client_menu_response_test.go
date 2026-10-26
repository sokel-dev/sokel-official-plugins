package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestMenuResponseEnvelopeCompatibility(t *testing.T) {
	cases := []struct {
		name, method, path, body string
		accepted                 bool
	}{
		{"normal success", "GET", "/cgi-bin/menu/get", `{"errcode":0,"errmsg":"ok","button":[{"name":"入口","type":"view","url":"https://example.com"}]}`, true},
		{"omitted errcode", "GET", "/cgi-bin/menu/get", `{"button":[{"name":"入口","type":"view","url":"https://example.com"}]}`, true},
		{"empty menu with errcode", "GET", "/cgi-bin/menu/get", `{"errcode":0,"button":[]}`, true},
		{"empty menu without errcode", "GET", "/cgi-bin/menu/get", `{"button":[]}`, true},
		{"error with button", "GET", "/cgi-bin/menu/get", `{"errcode":46003,"errmsg":"TEST-SECRET","button":[]}`, false},
		{"error without button", "GET", "/cgi-bin/menu/get", `{"errcode":46003,"errmsg":"TEST-SECRET"}`, false},
		{"null code", "GET", "/cgi-bin/menu/get", `{"errcode":null,"button":[]}`, false},
		{"null case insensitive code", "GET", "/cgi-bin/menu/get", `{"ErrCode":null,"button":[]}`, false},
		{"contradictory case insensitive code", "GET", "/cgi-bin/menu/get", `{"errcode":46003,"ErrCode":0,"button":[]}`, false},
		{"string code", "GET", "/cgi-bin/menu/get", `{"errcode":"0","button":[]}`, false},
		{"null response", "GET", "/cgi-bin/menu/get", `null`, false},
		{"array response", "GET", "/cgi-bin/menu/get", `[]`, false},
		{"missing buttons", "GET", "/cgi-bin/menu/get", `{}`, false},
		{"missing buttons with errcode", "GET", "/cgi-bin/menu/get", `{"errcode":0}`, false},
		{"null buttons", "GET", "/cgi-bin/menu/get", `{"button":null}`, false},
		{"null buttons with errcode", "GET", "/cgi-bin/menu/get", `{"errcode":0,"button":null}`, false},
		{"object buttons", "GET", "/cgi-bin/menu/get", `{"button":{}}`, false},
		{"null button item", "GET", "/cgi-bin/menu/get", `{"button":[null]}`, false},
		{"primitive button item", "GET", "/cgi-bin/menu/get", `{"button":[1]}`, false},
		{"malformed JSON", "GET", "/cgi-bin/menu/get", `{"button":[}`, false},
		{"trailing JSON", "GET", "/cgi-bin/menu/get", `{"button":[]} {}`, false},
		{"duplicate buttons", "GET", "/cgi-bin/menu/get", `{"button":[],"button":[]}`, false},
		{"other path", "GET", "/cgi-bin/agent/get", `{"button":[]}`, false},
		{"different method", "POST", "/cgi-bin/menu/get", `{"button":[]}`, false},
		{"different path suffix", "GET", "/cgi-bin/menu/get/", `{"button":[]}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestWeComClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != tc.method || r.URL.Path != tc.path {
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
				}
				io.WriteString(w, tc.body)
			})
			body, err := c.request(context.Background(), tc.method, tc.path, nil, nil)
			if (err == nil) != tc.accepted {
				t.Fatalf("accepted=%v, body=%s err=%v", tc.accepted, body, err)
			}
			if err != nil && strings.Contains(err.Error(), "TEST-SECRET") {
				t.Fatal("upstream error leaked response text")
			}
		})
	}
}

func TestGetMenuNotConfiguredHint(t *testing.T) {
	c := newTestWeComClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/cgi-bin/gettoken" {
			io.WriteString(w, documentedTokenResponse)
			return
		}
		io.WriteString(w, `{"errcode":46003,"errmsg":"menu no exist, hint: TEST-SECRET"}`)
	})
	_, err := c.GetMenu(context.Background(), clientTestCred())
	var apiErr *weComAPIError
	if !errors.As(err, &apiErr) || apiErr.code != 46003 || !strings.Contains(err.Error(), "当前应用尚未创建自定义菜单，请先设置菜单") || strings.Contains(err.Error(), "TEST-SECRET") {
		t.Fatalf("unconfigured-menu error was lost or unsafe: %v", err)
	}
}
