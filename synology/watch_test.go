package main

import (
	"context"
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// Ignore rules: Synology junk/temp files are always ignored; credential-appended substrings take effect; normal files pass through.
func TestIsIgnoredName(t *testing.T) {
	cases := []struct {
		name  string
		extra []string
		want  bool
	}{
		{"@eaDir", nil, true},
		{"#recycle", nil, true},
		{"Thumbs.db", nil, true},
		{"._report.pdf", nil, true}, // AppleDouble
		{"~$草稿.docx", nil, true},    // Office lock file
		{"a.tmp", nil, true},
		{"movie.part", nil, true},
		{"报告.pdf", nil, false},
		{"draft-x", []string{"draft"}, true},
		{"final.pdf", []string{"draft"}, false},
	}
	for _, c := range cases {
		if got := isIgnoredName(c.name, c.extra); got != c.want {
			t.Errorf("isIgnoredName(%q, %v) = %v, want %v", c.name, c.extra, got, c.want)
		}
	}
}

// Path jail: both absolute and relative paths are confined within root; .. traversal and an absolute path outside root are rejected.
func TestJailTo(t *testing.T) {
	root := "/watch/研报"
	if p, err := jailTo(root, "a/b.pdf"); err != nil || p != "/watch/研报/a/b.pdf" {
		t.Errorf("相对路径: %v %v", p, err)
	}
	if p, err := jailTo(root, "/watch/研报/x.pdf"); err != nil || p != "/watch/研报/x.pdf" {
		t.Errorf("界内绝对路径: %v %v", p, err)
	}
	if _, err := jailTo(root, "../其它/x.pdf"); err == nil {
		t.Error(".. 越界应拒绝")
	}
	if _, err := jailTo(root, "/etc/passwd"); err == nil {
		t.Error("界外绝对路径应拒绝")
	}
	if _, err := jailTo(root, "a/../../x"); err == nil {
		t.Error("内嵌 .. 越界应拒绝")
	}
}

// Credential parsing: include/ignore/settle are normalized; path is required.
func TestParseWatchCfg(t *testing.T) {
	cfg, err := parseWatchCfg(map[string]string{"path": "/watch/a/", "include": " PDF, .docx ", "settle_seconds": "5", "ignore": "draft, "})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.root != "/watch/a" || !cfg.include["pdf"] || !cfg.include["docx"] || cfg.settle != 5*time.Second || len(cfg.ignore) != 1 {
		t.Errorf("解析不符: %+v", cfg)
	}
	if !cfg.includeMatch("/watch/a/x.PDF") || cfg.includeMatch("/watch/a/x.txt") {
		t.Error("扩展名过滤不符")
	}
	if _, err := parseWatchCfg(map[string]string{}); err == nil {
		t.Error("缺 path 应报错")
	}
}

// End-to-end (real filesystem): a new file reports created once settled; an appended
// write reports changed; a delete reports deleted; a file that already existed at
// startup emits no event; a file inside an ignored directory is never reported.
func TestFSWatcherLifecycle(t *testing.T) {
	root := t.TempDir()
	// A file that already exists at startup: only inventoried, no event emitted.
	if err := os.WriteFile(filepath.Join(root, "已有.pdf"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "@eaDir"), 0o755); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	events := map[string][]string{} // kind -> paths
	fw := newFSWatcher(watchCfg{root: root, include: map[string]bool{}, settle: 150 * time.Millisecond},
		func(kind, path string, _ os.FileInfo) {
			mu.Lock()
			events[kind] = append(events[kind], filepath.Base(path))
			mu.Unlock()
		})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- fw.run(ctx) }()
	time.Sleep(200 * time.Millisecond) // wait for the watch to be set up

	// created: write → settle.
	target := filepath.Join(root, "新文件.txt")
	if err := os.WriteFile(target, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A file inside an ignored directory: not reported.
	if err := os.WriteFile(filepath.Join(root, "@eaDir", "SYNO.jpg"), []byte("t"), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(400 * time.Millisecond)

	// changed: an appended write.
	f, err := os.OpenFile(target, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(" world")
	_ = f.Close()
	time.Sleep(400 * time.Millisecond)

	// deleted.
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)

	cancel()
	<-done

	mu.Lock()
	defer mu.Unlock()
	has := func(kind, name string) bool {
		for _, n := range events[kind] {
			if n == name {
				return true
			}
		}
		return false
	}
	if !has("created", "新文件.txt") {
		t.Errorf("应报 created: %+v", events)
	}
	if !has("changed", "新文件.txt") {
		t.Errorf("应报 changed: %+v", events)
	}
	if !has("deleted", "新文件.txt") {
		t.Errorf("应报 deleted: %+v", events)
	}
	if len(events["created"]) > 1 || has("created", "已有.pdf") || has("created", "SYNO.jpg") {
		t.Errorf("初始文件/忽略目录不应发事件: %+v", events)
	}
}

// A newly created subdirectory dynamically gets a watch added: new files inside it also report created.
func TestFSWatcherNewSubdir(t *testing.T) {
	root := t.TempDir()
	var mu sync.Mutex
	var created []string
	fw := newFSWatcher(watchCfg{root: root, include: map[string]bool{}, settle: 150 * time.Millisecond},
		func(kind, path string, _ os.FileInfo) {
			if kind == "created" {
				mu.Lock()
				created = append(created, filepath.Base(path))
				mu.Unlock()
			}
		})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- fw.run(ctx) }()
	time.Sleep(200 * time.Millisecond)

	sub := filepath.Join(root, "2026-07")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond) // wait for the subdirectory watch to be added
	if err := os.WriteFile(filepath.Join(sub, "内部.pdf"), []byte("d"), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	cancel()
	<-done

	mu.Lock()
	defer mu.Unlock()
	found := false
	for _, n := range created {
		if n == "内部.pdf" {
			found = true
		}
	}
	if !found {
		t.Errorf("新子目录内文件应报 created: %v", created)
	}
}

// The event contract is generated from the schema declaration: three events, consistent
// common fields, and typed trigger functions exist.
//
// The point of this test is that "this used to be impossible to catch": events used to be
// declared imperatively with an untyped payload, so a misspelled event name or field name
// would only surface at runtime -- and as the hardest symptom to diagnose, "the event just
// doesn't fire."
func TestEventContractGenerated(t *testing.T) {
	h := &captureEventHost{}
	DeclareEvents(h)

	ids := map[string]bool{}
	for _, e := range h.events {
		ids[e.ID] = true
		if len(e.Fields) == 0 {
			t.Errorf("事件 %s 没有字段声明", e.ID)
		}
	}
	for _, want := range []string{"file_created", "file_changed", "file_deleted"} {
		if !ids[want] {
			t.Errorf("缺事件 %s", want)
		}
	}
	// Common fields: all four must be present, including on the delete event (it has no size/mtime, but the four common fields are mandatory).
	if len(h.common) != 4 {
		t.Fatalf("公共字段应有 4 个: %+v", h.commonNames)
	}
	for _, e := range h.events {
		for _, n := range h.commonNames {
			var found bool
			for _, f := range e.Fields {
				found = found || f.Name == n
			}
			if !found {
				t.Errorf("事件 %s 缺公共字段 %s", e.ID, n)
			}
		}
	}
}

type captureEventHost struct {
	events      []contract.Event
	common      []contract.Field
	commonNames []string
}

func (h *captureEventHost) DeclareEvent(e contract.Event) { h.events = append(h.events, e) }
func (h *captureEventHost) DeclareEventsCommon(fields []contract.Field, names []string) {
	h.common, h.commonNames = fields, names
}
