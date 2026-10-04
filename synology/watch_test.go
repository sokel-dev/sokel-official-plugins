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

// 忽略规则：Synology 垃圾/临时文件恒忽略；凭证追加子串生效；正常文件放行。
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
		{"~$草稿.docx", nil, true},    // Office 锁
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

// 路径 jail：绝对/相对都限制在 root 内；.. 越界与 root 外绝对路径拒绝。
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

// 凭证解析：include/ignore/settle 归一；path 必填。
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

// 端到端（真实文件系统）：新建文件落定后报 created；追加写报 changed；删除报 deleted；
// 初始已存在的文件不发事件；忽略目录里的文件不报。
func TestFSWatcherLifecycle(t *testing.T) {
	root := t.TempDir()
	// 初始已存在的文件：只盘点，不发事件。
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
	time.Sleep(200 * time.Millisecond) // 等 watch 建好

	// created：写入 → 落定。
	target := filepath.Join(root, "新文件.txt")
	if err := os.WriteFile(target, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 忽略目录中的文件：不报。
	if err := os.WriteFile(filepath.Join(root, "@eaDir", "SYNO.jpg"), []byte("t"), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(400 * time.Millisecond)

	// changed：追加写。
	f, err := os.OpenFile(target, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(" world")
	_ = f.Close()
	time.Sleep(400 * time.Millisecond)

	// deleted。
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

// 新建子目录动态补 watch：子目录里的新文件也能报 created。
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
	time.Sleep(200 * time.Millisecond) // 等子目录 watch 补上
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

// 事件契约由 schema 声明生成：三个事件、公共字段一致、typed 触发口存在。
//
// 这条的意义在于「以前根本做不到」：事件只能命令式声明 + 无类型 payload，
// 事件名拼错、字段名写错都要等运行期，而症状是「事件没触发」这种最难查的那类。
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
	// 公共字段：四个都得在，且删除事件也有（它没有 size/mtime，但公共的四个必须有）
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
