package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestInvalidInputsDoNotCallService(t *testing.T) {
	calls := 0
	s := newFixtureServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
	defer s.Close()
	t.Setenv("SOKEL_PROMETHEUS_URL", s.URL)
	for _, tc := range []struct {
		op string
		in any
	}{{"query", map[string]any{"query": " "}}, {"query", map[string]any{"query": "up", "options": map[string]any{"limit": 0}}}, {"query", map[string]any{"query": "up", "options": map[string]any{"time": "now"}}}, {"query_range", map[string]any{"query": "up", "start": "2026-11-04T00:00:00Z", "end": "2026-11-05T00:00:00Z", "step_seconds": "0.001"}}, {"query_range", map[string]any{"query": "up", "start": "2026-11-05T00:00:00Z", "end": "2026-11-04T00:00:00Z", "step_seconds": "10"}}, {"find_series", map[string]any{"options": map[string]any{}}}, {"list_label_values", map[string]any{"label": "../secrets"}}} {
		if _, e := dispatch(t, tc.op, tc.in, nil); e == nil {
			t.Fatalf("accepted %s %v", tc.op, tc.in)
		}
	}
	if calls != 0 {
		t.Fatal(calls)
	}
}
func TestResultVariantsAndMalformedShapes(t *testing.T) {
	good := []string{`{"resultType":"scalar","result":[1700000000.123456789,"+Inf"]}`, `{"resultType":"string","result":[1700000000,"hello"]}`, `{"resultType":"vector","result":[{"metric":{},"histogram":[1700000000,{"count":"3","sum":"4","buckets":[[0,"0","1","3"]]}]}]}`, `{"resultType":"matrix","result":[{"metric":{},"values":[],"histograms":[[1700000000,{"count":"0","sum":"0","buckets":[]}]]}]}`}
	for _, s := range good {
		if _, raw, e := queryResult([]byte(s), false); e != nil || raw == "" {
			t.Fatalf("valid result %v", e)
		}
	}
	bad := []string{`{}`, `{"resultType":"vector","result":null}`, `{"resultType":"vector","result":[{"metric":{},"value":[1,7]}]}`, `{"resultType":"matrix","result":[{"metric":{},"value":[1,"1"]}]}`, `{"resultType":"scalar","result":["1700000000","1"]}`, `{"resultType":"vector","result":[{"metric":{}}]}`}
	for _, s := range bad {
		if _, _, e := queryResult([]byte(s), false); e == nil {
			t.Errorf("malformed result accepted: %s", s)
		}
	}
}
func TestAuthResponseAndTransportBoundaries(t *testing.T) {
	for _, status := range []int{302, 401, 403, 429, 500} {
		calls := 0
		s := newFixtureServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			w.Header().Set("Location", "https://other.invalid")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			io.WriteString(w, `{"error":"SECRET"}`)
		}))
		t.Setenv("SOKEL_PROMETHEUS_URL", s.URL)
		_, e := dispatch(t, "get_build_info", map[string]any{}, nil)
		s.Close()
		if e == nil || strings.Contains(e.Error(), "SECRET") || calls != 1 {
			t.Fatalf("status %d calls%d error%v", status, calls, e)
		}
	}
	c := newClient(Cred{Username: "u", Password: "p", BearerToken: "t"})
	c.base = "http://private.local"
	if _, e := c.getBuildInfo(context.Background(), &GetBuildInfoIn{}); e == nil {
		t.Fatal("mixed auth accepted")
	}
	c = newClient(Cred{Username: "u", Password: "p"})
	c.base = "http://private.local/prefix"
	c.http = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		u, p, ok := r.BasicAuth()
		if !ok || u != "u" || p != "p" || r.GetBody != nil {
			t.Fatal("auth or POST replay regression")
		}
		return nil, errors.New("SECRET transport URL")
	})}
	if _, e := c.query(context.Background(), &QueryIn{Query: "up"}); e == nil || strings.Contains(e.Error(), "SECRET") {
		t.Fatal(e)
	}
}
func TestEnvelopeErrorsAndLimits(t *testing.T) {
	for _, body := range []string{`{"status":"error","data":{},"error":"SECRET"}`, `{"status":"success","data":null}`, `{"status":"success","data":{}} trailing`, strings.Repeat(" ", maxResponse+1)} {
		s := newFixtureServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, body)
		}))
		t.Setenv("SOKEL_PROMETHEUS_URL", s.URL)
		_, e := dispatch(t, "query", map[string]any{"query": "up"}, nil)
		s.Close()
		if e == nil || strings.Contains(e.Error(), "SECRET") {
			t.Fatalf("error %v", e)
		}
	}
}
