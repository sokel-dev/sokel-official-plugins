package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Reading a knowledge base whose index does not exist yet (never written, or dropped) answers "nothing", not an error:
// the platform asks a store whether it already holds a knowledge base before moving one in, and an
// index_not_found_exception there read as "the store did not answer".
func TestReadOfMissingIndexIsEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"root_cause":[{"type":"index_not_found_exception","reason":"no such index [sokel.kb.kb_x]"}],"type":"index_not_found_exception"},"status":404}`))
	}))
	defer srv.Close()
	e := &es{base: srv.URL, hc: srv.Client()}
	out, err := e.read(context.Background(), "POST", "/sokel.kb.kb_x/_search", map[string]any{})
	if err != nil || len(out) != 0 {
		t.Fatalf("索引不存在时读取应返回空，不是错误: %v %v", out, err)
	}

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"type":"parsing_exception"},"status":400}`))
	}))
	defer bad.Close()
	if _, err := (&es{base: bad.URL, hc: bad.Client()}).read(context.Background(), "POST", "/x/_search", nil); err == nil {
		t.Fatal("其它错误照常报出")
	}
}
