package main

// httptest fakes DSM to pin the wire shape (login form, multipart field order with the file last, the overwrite flag
// per conflict mode, logout) and the error translation. A real NAS is exercised through operation:test.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
)

type fakeCtx struct {
	context.Context
	cred      map[string]string
	files     map[string][]byte // platform file id → bytes
	fetchErrs []error           // errors the next Fetch calls return, in order, before succeeding
	fetches   int
}

func (f *fakeCtx) Credential() map[string]string { return f.cred }
func (f *fakeCtx) Upload(string, string, []byte) (*plugin.File, error) {
	return &plugin.File{ID: "f"}, nil
}
func (f *fakeCtx) UploadReader(string, string, io.Reader) (*plugin.File, error) {
	return &plugin.File{ID: "f"}, nil
}
func (f *fakeCtx) Fetch(pf *plugin.File) ([]byte, error) {
	f.fetches++
	if len(f.fetchErrs) > 0 {
		err := f.fetchErrs[0]
		f.fetchErrs = f.fetchErrs[1:]
		return nil, err
	}
	return f.files[pf.ID], nil
}

// upload is what the fake DSM saw for one upload request.
type upload struct {
	sid    string
	fields []string          // multipart part names, in order
	values map[string]string // non-file field values
	name   string
	body   string
}

// fakeDSM answers login / upload / logout. existing names make an upload hit a conflict: with overwrite absent DSM
// refuses (414, what a real DSM 7 answered; the guide says 1805), with overwrite=false it skips (blSkip), with true it overwrites.
type fakeDSM struct {
	mu        sync.Mutex
	loginCode int    // 0 = accept
	dropAll   bool   // every upload answers 119
	dropSID   string // an upload with this sid answers 119 (session gone), as DSM sometimes does mid-run
	existing  map[string]bool
	logins    []map[string]string
	uploads   []upload
	logouts   []string
}

func (d *fakeDSM) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	d.mu.Lock()
	defer d.mu.Unlock()
	fail := func(code int) {
		_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "error": map[string]int{"code": code}})
	}
	ok := func(data any) { _ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": data}) }
	q := r.URL.Query()
	switch {
	case r.URL.Path == "/webapi/auth.cgi" && r.Method == http.MethodPost:
		_ = r.ParseForm()
		form := map[string]string{}
		for k := range r.PostForm {
			form[k] = r.PostForm.Get(k)
		}
		form["_query"] = r.URL.RawQuery
		d.logins = append(d.logins, form)
		if d.loginCode != 0 {
			fail(d.loginCode)
			return
		}
		ok(map[string]string{"sid": fmt.Sprintf("sid-%d", len(d.logins))})
	case r.URL.Path == "/webapi/auth.cgi" && q.Get("method") == "logout":
		d.logouts = append(d.logouts, q.Get("_sid"))
		ok(nil)
	case r.URL.Path == "/webapi/entry.cgi" && q.Get("api") == "SYNO.FileStation.Upload":
		mr, err := r.MultipartReader()
		if err != nil {
			fail(101)
			return
		}
		u := upload{sid: q.Get("_sid"), values: map[string]string{}}
		for {
			p, err := mr.NextPart()
			if err != nil {
				break
			}
			b, _ := io.ReadAll(p)
			u.fields = append(u.fields, p.FormName())
			if p.FormName() == "file" {
				u.name, u.body = p.FileName(), string(b)
			} else {
				u.values[p.FormName()] = string(b)
			}
		}
		d.uploads = append(d.uploads, u)
		if d.dropAll || (d.dropSID != "" && u.sid == d.dropSID) {
			fail(119)
			return
		}
		if d.existing[u.name] {
			switch u.values["overwrite"] {
			case "":
				fail(414)
				return
			case "false":
				ok(map[string]any{"blSkip": true, "file": u.name})
				return
			}
		}
		ok(map[string]any{"blSkip": false, "file": u.name})
	default:
		http.NotFound(w, r)
	}
}

func newCtx(url string) *fakeCtx {
	return &fakeCtx{Context: context.Background(),
		cred:  map[string]string{"url": url, "username": "uploader", "password": "p@ss"},
		files: map[string][]byte{"f_a": []byte("AAA"), "f_b": []byte("BBBB")}}
}

var twoFiles = []*plugin.File{{ID: "f_a", Name: "a.pdf"}, {ID: "f_b", Name: "dir/b.pdf"}}

func TestUploadFile(t *testing.T) {
	cases := []struct {
		name          string
		onConflict    string
		existing      map[string]bool
		wantErr       string
		wantOverwrite []string // the overwrite field DSM saw, per upload ("" = absent)
		wantUploaded  int
		wantSkipped   int
	}{
		{name: "default mode uploads new files", onConflict: "", wantOverwrite: []string{"", ""}, wantUploaded: 2},
		{name: "skip marks the existing one", onConflict: "skip", existing: map[string]bool{"b.pdf": true},
			wantOverwrite: []string{"false", "false"}, wantUploaded: 1, wantSkipped: 1},
		{name: "overwrite sends true", onConflict: "overwrite", existing: map[string]bool{"a.pdf": true},
			wantOverwrite: []string{"true", "true"}, wantUploaded: 2},
		{name: "error mode reports the conflict and what was sent", onConflict: "error", existing: map[string]bool{"b.pdf": true},
			wantErr: "同名文件", wantOverwrite: []string{"", ""}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := &fakeDSM{existing: tc.existing}
			srv := httptest.NewServer(d)
			defer srv.Close()
			out, err := uploadFile(newCtx(srv.URL), &UploadFileIn{Files: twoFiles, Dir: "/research/入库/", OnConflict: tc.onConflict, CreateParents: true})
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) || !strings.Contains(err.Error(), "此前 1 个已传完") {
					t.Fatalf("want error naming %q and the files already sent, got %v", tc.wantErr, err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if len(d.uploads) != len(tc.wantOverwrite) {
				t.Fatalf("uploads: want %d got %d", len(tc.wantOverwrite), len(d.uploads))
			}
			for i, u := range d.uploads {
				if u.fields[len(u.fields)-1] != "file" {
					t.Errorf("upload %d: the file part must come last, got %v", i, u.fields)
				}
				if u.values["overwrite"] != tc.wantOverwrite[i] || u.values["path"] != "/research/入库" || u.values["create_parents"] != "true" || u.sid != "sid-1" {
					t.Errorf("upload %d fields: %+v sid=%s", i, u.values, u.sid)
				}
			}
			if d.uploads[1].name != "b.pdf" || d.uploads[1].body != "BBBB" {
				t.Errorf("second file: name %q body %q (the name keeps only its base)", d.uploads[1].name, d.uploads[1].body)
			}
			if len(d.logouts) != 1 || d.logouts[0] != "sid-1" {
				t.Errorf("the session must be closed once, even on failure: %v", d.logouts)
			}
			if err != nil {
				return
			}
			if out.Uploaded != tc.wantUploaded || out.Skipped != tc.wantSkipped || len(out.Files) != 2 {
				t.Errorf("counts: uploaded %d skipped %d files %d", out.Uploaded, out.Skipped, len(out.Files))
			}
			if out.Files[0].Path != "/research/入库/a.pdf" || out.Files[0].Size != 3 {
				t.Errorf("result: %+v", out.Files[0])
			}
		})
	}
}

// The password goes in a POST body, never the URL (access logs keep URLs).
func TestLoginKeepsPasswordOutOfURL(t *testing.T) {
	d := &fakeDSM{}
	srv := httptest.NewServer(d)
	defer srv.Close()
	if _, err := uploadFile(newCtx(srv.URL), &UploadFileIn{Files: twoFiles[:1], Dir: "/research"}); err != nil {
		t.Fatal(err)
	}
	l := d.logins[0]
	if l["passwd"] != "p@ss" || l["account"] != "uploader" || l["session"] != "FileStation" || strings.Contains(l["_query"], "p@ss") {
		t.Errorf("login: %v", l)
	}
}

func TestUploadRejectsBadInput(t *testing.T) {
	srv := httptest.NewServer(&fakeDSM{})
	defer srv.Close()
	for _, dir := range []string{"", "/", "research/x"} {
		if _, err := uploadFile(newCtx(srv.URL), &UploadFileIn{Files: twoFiles, Dir: dir}); err == nil || !strings.Contains(err.Error(), "共享文件夹") {
			t.Errorf("dir %q: want a shared-folder error, got %v", dir, err)
		}
	}
	ctx := newCtx(srv.URL)
	ctx.cred["url"] = "nas:5001"
	if _, err := uploadFile(ctx, &UploadFileIn{Files: twoFiles, Dir: "/research"}); err == nil || !strings.Contains(err.Error(), "带协议") {
		t.Errorf("url without scheme: %v", err)
	}
}

func TestHealthCheck(t *testing.T) {
	cases := []struct {
		name      string
		loginCode int
		wantOK    bool
		wantMsg   string
	}{
		{"accepted", 0, true, "已登录"},
		{"wrong password", 400, false, "账号或密码不对"},
		{"two-step verification", 403, false, "两步验证"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := &fakeDSM{loginCode: tc.loginCode}
			srv := httptest.NewServer(d)
			defer srv.Close()
			out, err := healthCheck(newCtx(srv.URL), &HealthCheckIn{})
			if err != nil {
				t.Fatalf("unavailable is ok=false, not an error: %v", err)
			}
			if out.OK != tc.wantOK || !strings.Contains(out.Message, tc.wantMsg) {
				t.Errorf("got ok=%v %q", out.OK, out.Message)
			}
		})
	}
}

// A failure outside DSM's envelope is reported as such, not as an empty success.
func TestNonDSMAnswers(t *testing.T) {
	cases := []struct {
		name    string
		handler http.HandlerFunc
		wantMsg string
	}{
		{"server error", func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "boom", http.StatusInternalServerError) }, "HTTP 500"},
		{"not DSM", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("<html>router login</html>")) }, "地址是否指向 DSM"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()
			out, err := healthCheck(newCtx(srv.URL), &HealthCheckIn{})
			if err != nil || out.OK || !strings.Contains(out.Message, tc.wantMsg) {
				t.Errorf("got ok=%v %q err=%v", out.OK, out.Message, err)
			}
		})
	}
}

// A session DSM drops mid-call (119; 106 / 107 alike) gets one fresh login and the file is sent again, instead of
// failing the report: seen on a real DSM with uploads running side by side (2026-10-13).
func TestUploadRetriesOnLostSession(t *testing.T) {
	d := &fakeDSM{dropSID: "sid-1"}
	srv := httptest.NewServer(d)
	defer srv.Close()
	out, err := uploadFile(newCtx(srv.URL), &UploadFileIn{Files: twoFiles, Dir: "/research", OnConflict: "error"})
	if err != nil {
		t.Fatal(err)
	}
	if len(d.logins) != 2 || len(d.uploads) != 3 || d.uploads[1].sid != "sid-2" || d.uploads[2].sid != "sid-2" {
		t.Errorf("want a second login and the rest on its session: logins %d, uploads %+v", len(d.logins), d.uploads)
	}
	if out.Uploaded != 2 {
		t.Errorf("uploaded %d", out.Uploaded)
	}
	if len(d.logouts) != 1 || d.logouts[0] != "sid-2" {
		t.Errorf("the live session is the one logged out: %v", d.logouts)
	}
}

// A session that is lost again right after the fresh login fails the call (no retry loop).
func TestUploadGivesUpAfterOneRelogin(t *testing.T) {
	d := &fakeDSM{dropSID: "*"}
	srv := httptest.NewServer(d)
	defer srv.Close()
	d.dropAll = true
	_, err := uploadFile(newCtx(srv.URL), &UploadFileIn{Files: twoFiles[:1], Dir: "/research"})
	if err == nil || !strings.Contains(err.Error(), "会话无效") || len(d.logins) != 2 {
		t.Errorf("want one retry then the session error, got %v after %d logins", err, len(d.logins))
	}
}

// Reading the platform file times out now and then (a chunk request not answered in 30s, seen on 2026-10-13 in a
// batch run); the read is tried again a couple of times. Other errors (a file that is gone) fail at once.
func TestFetchRetries(t *testing.T) {
	fetchRetryGap = 0
	cases := []struct {
		name        string
		errs        []error
		wantErr     string
		wantFetches int
	}{
		{"timeouts then success", []error{errors.New("fetching chunk 0: nats: timeout"), errors.New("fetching chunk 3: nats: timeout")}, "", 3},
		{"keeps timing out", []error{errors.New("nats: timeout"), errors.New("nats: timeout"), errors.New("nats: timeout")}, "nats: timeout", 3},
		{"not found is not retried", []error{errors.New("file not found")}, "file not found", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(&fakeDSM{})
			defer srv.Close()
			ctx := newCtx(srv.URL)
			ctx.fetchErrs = tc.errs
			_, err := uploadFile(ctx, &UploadFileIn{Files: twoFiles[:1], Dir: "/research"})
			if tc.wantErr == "" && err != nil || tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)) {
				t.Errorf("err = %v, want %q", err, tc.wantErr)
			}
			if ctx.fetches != tc.wantFetches {
				t.Errorf("fetches = %d, want %d", ctx.fetches, tc.wantFetches)
			}
		})
	}
}
