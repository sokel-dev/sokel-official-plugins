package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestTransportLimitsAndCredentials(t *testing.T) {
	for _, key := range []string{"", "key\r\nInjected: secret"} {
		if _, err := newAPIClient("TEST_DOC_BASE", "https://example.com", key, true, time.Second); err == nil {
			t.Fatal("invalid credential accepted")
		}
	}
	var calls atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{}`)
	}))
	defer s.Close()
	t.Setenv("TEST_DOC_BASE", s.URL)
	c, err := newAPIClient("TEST_DOC_BASE", "", "test-secret", true, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err = c.request(context.Background(), "POST", "/", map[string]string{"large": strings.Repeat("x", maxRequestBytes)}, nil, &out); err == nil {
		t.Fatal("oversized request accepted")
	}
	if calls.Load() != 0 {
		t.Fatal("oversized request contacted upstream")
	}
	if err = c.request(context.Background(), "POST", "/", make(chan int), nil, &out); err == nil {
		t.Fatal("unencodable request accepted")
	}
	if _, err = pageURL("https://example.com/" + strings.Repeat("x", 8192)); err == nil {
		t.Fatal("oversized URL accepted")
	}
}
func TestTransportStatusAndTimeout(t *testing.T) {
	for _, tc := range []struct {
		name, content string
		status        int
	}{{"forbidden", "application/json", 403}, {"credits", "application/json", 402}, {"html", "text/html", 200}} {
		t.Run(tc.name, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tc.content)
				w.WriteHeader(tc.status)
				io.WriteString(w, "test-secret")
			}))
			defer s.Close()
			t.Setenv("TEST_DOC_BASE", s.URL)
			c, _ := newAPIClient("TEST_DOC_BASE", "", "test-secret", true, time.Second)
			var out any
			err := c.request(context.Background(), "GET", "/", nil, nil, &out)
			if err == nil || strings.Contains(err.Error(), "test-secret") {
				t.Fatalf("missing or unsafe error: %v", err)
			}
		})
	}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(100 * time.Millisecond):
		}
	}))
	defer s.Close()
	t.Setenv("TEST_DOC_BASE", s.URL)
	c, _ := newAPIClient("TEST_DOC_BASE", "", "test-secret", true, 10*time.Millisecond)
	var out any
	err := c.request(context.Background(), "GET", "/", nil, nil, &out)
	if err == nil || strings.Contains(err.Error(), "test-secret") {
		t.Fatalf("timeout not safely returned: %v", err)
	}
}
