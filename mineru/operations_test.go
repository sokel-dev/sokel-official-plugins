package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Published API examples plus explicitly synthetic edge cases; not live-account fixtures.
func TestDocumentedWireRequests(t *testing.T) {
	runCases(t, []wireCase{
		{"submit", `{"url":"https://cdn-mineru.openxlab.org.cn/demo/example.pdf","model_version":"vlm","options":{"enable_formula":false}}`, "POST", "/api/v4/extract/task", `{"url":"https://cdn-mineru.openxlab.org.cn/demo/example.pdf","model_version":"vlm","enable_formula":false}`, `{"code":0,"data":{"task_id":"a90e6ab6-44f3-4554-b459-b62fe4c6b43605"},"msg":"ok","trace_id":"trace-1"}`, func(t *testing.T, v any) {
			if v.(*SubmitOut).TaskID == "" {
				t.Fatal(v)
			}
		}},
		{"status", `{"task_id":"task-1"}`, "GET", "/api/v4/extract/task/task-1", "", `{"code":0,"data":{"task_id":"task-1","state":"done","full_zip_url":"https://cdn-mineru.openxlab.org.cn/pdf/example.zip","extract_progress":{"extracted_pages":2,"total_pages":2}},"msg":"ok","trace_id":"trace-1"}`, func(t *testing.T, v any) {
			o := v.(*StatusOut)
			if o.Result.State != "done" || o.Result.ExtractProgress.TotalPages != 2 || o.Result.FullZIPURL == "" {
				t.Fatal(o)
			}
		}},
		{"batch_submit", `{"files":[{"url":"https://cdn-mineru.openxlab.org.cn/demo/example.pdf","data_id":"abcd","is_ocr":false}],"model_version":"vlm"}`, "POST", "/api/v4/extract/task/batch", `{"files":[{"url":"https://cdn-mineru.openxlab.org.cn/demo/example.pdf","data_id":"abcd","is_ocr":false}],"model_version":"vlm"}`, `{"code":0,"data":{"batch_id":"2bb2f0ec-a336-4a0a-b61a-241afaf9cc87"},"msg":"ok","trace_id":"trace-1"}`, nil},
		{"batch_status", `{"batch_id":"batch-1"}`, "GET", "/api/v4/extract-results/batch/batch-1", "", `{"code":0,"data":{"batch_id":"batch-1","extract_result":[{"file_name":"demo.pdf","state":"failed","err_msg":"文件格式不支持"}]},"msg":"ok"}`, func(t *testing.T, v any) {
			o := v.(*BatchStatusOut)
			if len(o.Results) != 1 || o.Results[0].State != "failed" {
				t.Fatal(o)
			}
		}},
		{"health_check", `{"task_id":"task-1"}`, "GET", "/api/v4/extract/task/task-1", "", `{"code":0,"data":{"task_id":"task-1","state":"pending"}}`, func(t *testing.T, v any) {
			if !v.(*HealthCheckOut).OK {
				t.Fatal(v)
			}
		}},
	}, []string{"SOKEL_MINERU_BASE_URL"})
}
func TestBoundaries(t *testing.T) {
	runBoundaries(t, "SOKEL_MINERU_BASE_URL", "submit", `{"url":"https://example.com/a.pdf"}`, "")
}
func TestInvalidAndBusinessErrors(t *testing.T) {
	for _, x := range []struct{ op, in string }{{"submit", `{"url":"file:///etc/passwd"}`}, {"submit", `{"url":"https://example.com/a.pdf","model_version":"invented"}`}, {"submit", `{"url":"https://example.com/a.pdf","extra_formats":["zip"]}`}, {"batch_submit", `{"files":[]}`}, {"status", `{"task_id":"../account"}`}} {
		_, e := invoke(context.Background(), "test-secret", x.op, x.in)
		if e == nil {
			t.Fatal(x)
		}
	}
	for _, body := range []string{`{"code":"A0202","msg":"test-secret"}`, `{"code":-60002,"msg":"test-secret"}`, `{}`, `{"code":0,"data":null}`} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, body)
		}))
		t.Setenv("SOKEL_MINERU_BASE_URL", s.URL)
		_, e := invoke(context.Background(), "test-secret", "submit", `{"url":"https://example.com/a.pdf"}`)
		if e == nil || strings.Contains(e.Error(), "test-secret") {
			t.Fatalf("bad error %v", e)
		}
		s.Close()
	}
	v, e := invoke(context.Background(), "test-secret", "health_check", `{}`)
	if e != nil || v.(*HealthCheckOut).OK {
		t.Fatal("missing task falsely verified")
	}
}

func TestEveryOperationRejectsMissingCredentialsAndIncompleteResponses(t *testing.T) {
	inputs := map[string]string{"submit": `{"url":"https://example.com/a.pdf"}`, "status": `{"task_id":"task-1"}`, "batch_submit": `{"files":[{"url":"https://example.com/a.pdf"}]}`, "batch_status": `{"batch_id":"batch-1"}`}
	for op, input := range inputs {
		if _, e := invoke(context.Background(), "", op, input); e == nil {
			t.Fatalf("%s accepted missing key", op)
		}
	}
	for _, response := range []string{`{"code":0,"data":{}}`, `{"code":0,"data":[]}`, `{"code":"test-secret","msg":"test-secret"}`} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, response)
		}))
		t.Setenv("SOKEL_MINERU_BASE_URL", s.URL)
		for op, input := range inputs {
			if _, e := invoke(context.Background(), "test-secret", op, input); e == nil || strings.Contains(e.Error(), "test-secret") {
				t.Fatalf("%s invalid response: %v", op, e)
			}
		}
		v, e := invoke(context.Background(), "test-secret", "health_check", `{"task_id":"task-1"}`)
		if e != nil || v.(*HealthCheckOut).OK {
			t.Fatal("bad response falsely verified health")
		}
		s.Close()
	}
}
func TestOmissionAndErrorRedaction(t *testing.T) {
	runCases(t, []wireCase{{"submit", `{"url":"https://example.com/a.pdf","page_ranges":"2--2"}`, "POST", "/api/v4/extract/task", `{"url":"https://example.com/a.pdf","page_ranges":"2--2"}`, `{"code":"0","data":{"task_id":"task-1"}}`, nil}, {"status", `{"task_id":"task-1"}`, "GET", "/api/v4/extract/task/task-1", "", `{"code":0,"data":{"state":"failed","err_msg":"credential test-secret was rejected"},"trace_id":"test-secret"}`, func(t *testing.T, v any) {
		o := v.(*StatusOut)
		if o.Result.TaskID != "task-1" || strings.Contains(o.Result.ErrMsg, "test-secret") || strings.Contains(o.TraceID, "test-secret") {
			t.Fatal("result did not redact diagnostic fields")
		}
	}}}, []string{"SOKEL_MINERU_BASE_URL"})
}
