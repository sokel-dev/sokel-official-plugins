package main

import (
	"context"
	"github.com/sokel-dev/sokel-official-plugins/tianyancha/schema"
	"net/http"
	"strings"
	"testing"
)

// These payloads are synthetic projections of official API 365 and 822 examples.
func TestCompanyWireAndExactIDs(t *testing.T) {
	a := fakeAPI(t, func(r *http.Request) string {
		if r.Method != "GET" || r.URL.Host != "open.api.tianyancha.com" || r.URL.Path != "/services/v4/open/baseinfoV3" || r.URL.Query().Get("keyword") != "示例+公司" {
			t.Fatal(r.URL)
		}
		return `{"error_code":0,"reason":"ok","result":{"id":9007199254740993,"name":"Example Ltd","regStatus":"Active","creditCode":"CODE","legalPersonName":"Example","regCapital":"10万元","estiblishTime":1700000000000,"staffList":{"total":2,"result":[{"id":9007199254740995,"name":"Director","typeJoin":["Director"]}]}}}`
	})
	o, e := a.GetCompany(context.Background(), &GetCompanyIn{Keyword: "示例+公司"})
	if e != nil {
		t.Fatal(e)
	}
	if o.Company.ID != "9007199254740993" || o.Company.Staff[0].ID != "9007199254740995" || o.Company.StaffTotal != 2 || o.Company.EstablishedAt != "1700000000000" {
		t.Fatal(o)
	}
}
func TestChangesPaginationWire(t *testing.T) {
	a := fakeAPI(t, func(r *http.Request) string {
		q := r.URL.Query()
		if r.URL.Path != "/services/open/ic/changeinfo/2.0" || q.Get("keyword") != "Company" || q.Get("pageNum") != "2" || q.Get("pageSize") != "1" {
			t.Fatal(r.URL)
		}
		return `{"error_code":0,"result":{"total":3,"items":[{"changeItem":"Name","contentBefore":"Old","contentAfter":"New","changeTime":"2026-01-01","createTime":"2026-01-02"}]}}`
	})
	o, e := a.ListCompanyChanges(context.Background(), &ListCompanyChangesIn{Keyword: "Company", Page: schema.PageOptions{Page: ptr(2), PageSize: ptr(1)}})
	if e != nil || !o.HasMore || o.NextPage != 3 || o.Changes[0].After != "New" {
		t.Fatal(o, e)
	}
}
func TestNoResultsAndBusinessErrors(t *testing.T) {
	a := fakeAPI(t, func(*http.Request) string { return `{"error_code":300000,"reason":"no results","result":null}` })
	o, e := a.ListCompanyChanges(context.Background(), &ListCompanyChangesIn{Keyword: "Company"})
	if e != nil || o.Changes == nil || len(o.Changes) != 0 || o.HasMore {
		t.Fatal(o, e)
	}
	if _, e = a.GetCompany(context.Background(), &GetCompanyIn{Keyword: "Company"}); e == nil {
		t.Fatal("missing company accepted")
	}
	for _, code := range []string{"300002", "300003", "300004", "300005", "300006", "300007", "300011"} {
		a = fakeAPI(t, func(*http.Request) string { return `{"error_code":` + code + `,"reason":"test-secret"}` })
		if _, e = a.GetCompany(context.Background(), &GetCompanyIn{Keyword: "Company"}); e == nil || strings.Contains(e.Error(), "test-secret") {
			t.Fatal(code, e)
		}
	}
}
func TestMalformedSuccessRejected(t *testing.T) {
	for _, raw := range []string{`{}`, `{"error_code":0}`, `{"error_code":0,"result":{}}`, `{"error_code":0,"result":{"id":true,"name":"X"}}`} {
		a := fakeAPI(t, func(*http.Request) string { return raw })
		if _, e := a.GetCompany(context.Background(), &GetCompanyIn{Keyword: "Company"}); e == nil {
			t.Fatal(raw)
		}
	}
	for _, raw := range []string{`{"error_code":0,"result":{"total":2,"items":[]}}`, `{"error_code":0,"result":{"total":0}}`, `{"error_code":0,"result":{"total":1,"items":[{}]}}`} {
		a := fakeAPI(t, func(*http.Request) string { return raw })
		if _, e := a.ListCompanyChanges(context.Background(), &ListCompanyChangesIn{Keyword: "Company"}); e == nil {
			t.Fatal(raw)
		}
	}
}
func TestInvalidQueryDoesNotSend(t *testing.T) {
	a := fakeAPI(t, func(*http.Request) string { t.Fatal("invalid request sent"); return "{}" })
	if _, e := a.GetCompany(context.Background(), &GetCompanyIn{}); e == nil {
		t.Fatal("empty keyword")
	}
	if _, e := a.ListCompanyChanges(context.Background(), &ListCompanyChangesIn{Keyword: "Company", Page: schema.PageOptions{Page: ptr(0)}}); e == nil {
		t.Fatal("page0")
	}
}
func TestPreEpochEstablishmentDate(t *testing.T) {
	a := fakeAPI(t, func(*http.Request) string {
		return `{"error_code":0,"result":{"id":1,"name":"Company","estiblishTime":-631152000000}}`
	})
	o, e := a.GetCompany(context.Background(), &GetCompanyIn{Keyword: "Company"})
	if e != nil || o.Company.EstablishedAt != "-631152000000" {
		t.Fatal(o, e)
	}
}
