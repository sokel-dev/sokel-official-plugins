package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// Synthetic protocol fixtures derived from the linked official API documentation; not live captures.
type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func fakeAPI(t *testing.T, f func(*http.Request) (int, string)) *api {
	t.Helper()
	a := newAPI(testCred())
	a.client = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		code, body := f(r)
		return &http.Response{StatusCode: code, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}
	return a
}
func bodyOf(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	var v map[string]any
	if e := json.NewDecoder(r.Body).Decode(&v); e != nil {
		t.Fatal(e)
	}
	return v
}
func testCred() Cred {
	return Cred{InstanceURL: "https://example.my.salesforce.com", AccessToken: "test-secret"}
}
func TestProtocol(t *testing.T) {
	calls := 0
	a := fakeAPI(t, func(r *http.Request) (int, string) {
		calls++
		if r.URL.Host != "example.my.salesforce.com" || r.Header.Get("Authorization") != "Bearer test-secret" {
			t.Fatal("invalid auth/origin")
		}
		if r.URL.Path == "/services/data/v65.0/limits" {
			return 200, `{"DailyApiRequests":{"Max":1000,"Remaining":999}}`
		}
		if r.Method != "PATCH" || r.URL.Path != "/services/data/v65.0/sobjects/Account/External_ID__c/ext-1" {
			t.Fatal(r.URL, r.Method)
		}
		if bodyOf(t, r)["Name"] != "Changed" {
			t.Fatal("field lost")
		}
		return 200, `{"id":"001000000000001AAA","success":true,"errors":[],"created":false}`
	})
	h, e := a.health_check(context.Background(), &HealthCheckIn{})
	if e != nil || h == nil || !h.OK || calls != 1 {
		t.Fatalf("health: %#v %v calls=%d", h, e, calls)
	}
	v, e := a.upsert_record(context.Background(), &UpsertRecordIn{ObjectName: "Account", ExternalIDField: "External_ID__c", ExternalID: "ext-1", Fields: map[string]any{"Name": "Changed"}})
	if e != nil || v == nil || !v.Success || v.Created {
		t.Fatalf("upsert: %#v %v", v, e)
	}
}
