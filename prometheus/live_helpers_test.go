package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type capturedExchange struct {
	Method   string          `json:"method"`
	Path     string          `json:"path"`
	Request  string          `json:"request"`
	Status   int             `json:"status"`
	Response json.RawMessage `json:"response"`
}

func captureLocalHTTP(t *testing.T, name string) {
	t.Helper()
	dir := os.Getenv("SOKEL_TEST_CAPTURE_DIR")
	if dir == "" {
		return
	}
	clean := filepath.Clean(dir)
	if !strings.HasPrefix(clean, "/tmp/") && !strings.HasPrefix(clean, "/private/tmp/") {
		t.Fatal("capture directory must be under /tmp")
	}
	old := http.DefaultTransport
	var rows []capturedExchange
	var mu sync.Mutex
	http.DefaultTransport = roundTrip(func(r *http.Request) (*http.Response, error) {
		var req []byte
		if r.Body != nil {
			req, _ = io.ReadAll(r.Body)
			r.Body = io.NopCloser(bytes.NewReader(req))
		}
		resp, e := old.RoundTrip(r)
		if e != nil {
			return resp, e
		}
		b, e := io.ReadAll(resp.Body)
		resp.Body.Close()
		if e != nil {
			return nil, e
		}
		resp.Body = io.NopCloser(bytes.NewReader(b))
		raw := json.RawMessage(b)
		if len(b) == 0 {
			raw = json.RawMessage("null")
		}
		mu.Lock()
		rows = append(rows, capturedExchange{r.Method, r.URL.RequestURI(), string(req), resp.StatusCode, raw})
		mu.Unlock()
		return resp, nil
	})
	t.Cleanup(func() {
		http.DefaultTransport = old
		if e := os.MkdirAll(clean, 0700); e != nil {
			t.Error(e)
			return
		}
		b, e := json.MarshalIndent(rows, "", "  ")
		if e != nil {
			t.Error(e)
			return
		}
		if e = os.WriteFile(filepath.Join(clean, name+"-generated-captures.json"), append(b, '\n'), 0600); e != nil {
			t.Error(e)
		}
	})
}
