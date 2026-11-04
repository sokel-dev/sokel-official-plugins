package main

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

type testContext struct {
	context.Context
	key string
}

func (c testContext) Credential() map[string]string { return map[string]string{"api_key": c.key} }
func (c testContext) Upload(string, string, []byte) (*plugin.File, error) {
	return nil, errors.New("unused")
}
func (c testContext) UploadReader(string, string, io.Reader) (*plugin.File, error) {
	return nil, errors.New("unused")
}
func (c testContext) Fetch(*plugin.File) ([]byte, error) { return nil, errors.New("unused") }

type testHost map[string]plugin.Invoke

func (h testHost) Register(o contract.Operation, f plugin.Invoke) { h[o.ID] = f }

type testSink struct{ value any }

func (s *testSink) Vars(v any)  { s.value = v }
func (s *testSink) Text(string) {}
func (s *testSink) JSON(any)    {}
func invoke(ctx context.Context, key, op, input string) (any, error) {
	h := testHost{}
	register(h)
	s := &testSink{}
	e := h[op](testContext{ctx, key}, json.RawMessage(input), s)
	return s.value, e
}
func equalJSON(t *testing.T, got, want string) {
	t.Helper()
	var a, b any
	if json.Unmarshal([]byte(got), &a) != nil || json.Unmarshal([]byte(want), &b) != nil || !reflect.DeepEqual(a, b) {
		t.Errorf("wire JSON = %s; want %s", got, want)
	}
}

type wireCase struct {
	op, input, method, path, body, response string
	check                                   func(*testing.T, any)
}

func runCases(t *testing.T, cases []wireCase, envs []string) {
	t.Helper()
	for _, tt := range cases {
		t.Run(tt.op, func(t *testing.T) {
			calls := 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != tt.method || r.URL.RequestURI() != tt.path {
					t.Errorf("wire route %s %s", r.Method, r.URL.RequestURI())
				}
				if r.Header.Get("Authorization") != "Bearer test-secret" {
					t.Error("missing bearer key")
				}
				if r.Header.Get("Accept") != "application/json" {
					t.Error("missing JSON Accept")
				}
				b, _ := io.ReadAll(r.Body)
				if tt.body != "" {
					if r.Header.Get("Content-Type") != "application/json" {
						t.Error("missing JSON content type")
					}
					equalJSON(t, string(b), tt.body)
				}
				w.Header().Set("Content-Type", "application/json")
				io.WriteString(w, tt.response)
			}))
			defer s.Close()
			for _, e := range envs {
				t.Setenv(e, s.URL)
			}
			out, err := invoke(context.Background(), "test-secret", tt.op, tt.input)
			if err != nil {
				t.Fatalf("operation failed: %v", err)
			}
			if calls != 1 {
				t.Fatalf("calls=%d, want 1", calls)
			}
			if tt.check != nil {
				tt.check(t, out)
			}
		})
	}
}
func runBoundaries(t *testing.T, env, op, input, good string) {
	t.Helper()
	for _, tc := range []struct {
		name, body string
		code       int
	}{{"unauthorized", "test-secret", 401}, {"rate_limit", "test-secret", 429}, {"server_error", "test-secret", 500}, {"malformed", `{"secret":"test-secret"`, 200}, {"oversized", strings.Repeat("x", 16*1024*1024+1), 200}} {
		t.Run(tc.name, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.code)
				io.WriteString(w, tc.body)
			}))
			defer s.Close()
			t.Setenv(env, s.URL)
			_, e := invoke(context.Background(), "test-secret", op, input)
			if e == nil || strings.Contains(e.Error(), "test-secret") {
				t.Fatalf("unsafe or missing error: %v", e)
			}
		})
	}
	t.Run("cancellation", func(t *testing.T) {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			io.Copy(io.Discard, r.Body)
			select {
			case <-r.Context().Done():
			case <-time.After(250 * time.Millisecond):
			}
		}))
		defer s.Close()
		t.Setenv(env, s.URL)
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		_, e := invoke(ctx, "test-secret", op, input)
		if e == nil || strings.Contains(e.Error(), "test-secret") {
			t.Fatalf("cancellation error=%v", e)
		}
	})
	t.Run("redirect_never_forwards_key", func(t *testing.T) {
		calls := 0
		other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; t.Error("redirect destination contacted") }))
		defer other.Close()
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, other.URL, http.StatusTemporaryRedirect)
		}))
		defer s.Close()
		t.Setenv(env, s.URL)
		_, e := invoke(context.Background(), "test-secret", op, input)
		if e == nil || calls != 0 {
			t.Fatalf("redirect error=%v calls=%d", e, calls)
		}
	})
	t.Run("unsafe_deployment_url", func(t *testing.T) {
		for _, base := range []string{"http://example.com", "https://user:pass@example.com", "https://example.com?token=bad", "ftp://example.com"} {
			t.Setenv(env, base)
			_, e := invoke(context.Background(), "test-secret", op, input)
			if e == nil {
				t.Fatalf("accepted %s", base)
			}
		}
	})
}
