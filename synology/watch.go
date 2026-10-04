// File-change watching source (per-credential: one credential = one watched subdirectory
// = one instance of this file running).
//
// Design notes (driven by real-world Synology/SMB behavior, see README):
//   - fsnotify doesn't recurse: the initial walk adds a watch on the whole tree, and new
//     subdirectories get one added dynamically; moving a whole directory in backfills
//     "created" for its inner files.
//   - Settle detection: an SMB client save is temp-file+rename, a large file copy is
//     create+continuous write — a file is only reported once it's been silent for
//     `settle` seconds with a stable size, otherwise the workflow would get a half-written file.
//   - Synology junk is always ignored: @eaDir (thumbnails) / #recycle (recycle bin) / @*
//     system directories / Office lock files / ._* AppleDouble.
//   - When the inotify watch limit (DSM defaults to 8192) is hit, Add returns "no space":
//     this is surfaced as an error on the status board pointing at the sysctl fix, never
//     swallowed silently.
package main

import (
	"context"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
)

// Built-in ignores (Synology/system noise): an exact or prefix match on a directory or
// file name ignores the whole subtree.
var builtinIgnoreExact = map[string]bool{
	"#recycle": true, "#snapshot": true, ".SynologyWorkingDirectory": true,
	"Thumbs.db": true, ".DS_Store": true, "desktop.ini": true,
}
var builtinIgnorePrefix = []string{"@", "._", "~$"} // system directories like @eaDir/@tmp, AppleDouble, Office locks
var builtinIgnoreSuffix = []string{".tmp", ".part", ".crdownload", ".swp", ".download"}

// isIgnoredName: name-level ignoring: built-in rules + substring patterns appended by the credential.
func isIgnoredName(name string, extra []string) bool {
	if builtinIgnoreExact[name] {
		return true
	}
	for _, p := range builtinIgnorePrefix {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	for _, s := range builtinIgnoreSuffix {
		if strings.HasSuffix(strings.ToLower(name), s) {
			return true
		}
	}
	for _, sub := range extra {
		if sub != "" && strings.Contains(name, sub) {
			return true
		}
	}
	return false
}

type watchCfg struct {
	root    string
	include map[string]bool // lowercase extensions (no dot); empty = all
	ignore  []string        // extra ignore substrings
	settle  time.Duration
}

func parseWatchCfg(cred map[string]string) (watchCfg, error) {
	root := filepath.Clean(strings.TrimSpace(cred["path"]))
	if root == "" || root == "." {
		return watchCfg{}, fmt.Errorf("凭证未配置监听目录 path")
	}
	cfg := watchCfg{root: root, include: map[string]bool{}, settle: 2 * time.Second}
	for _, e := range strings.Split(cred["include"], ",") {
		if e = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(e), ".")); e != "" {
			cfg.include[e] = true
		}
	}
	for _, s := range strings.Split(cred["ignore"], ",") {
		if s = strings.TrimSpace(s); s != "" {
			cfg.ignore = append(cfg.ignore, s)
		}
	}
	if n, err := strconv.Atoi(strings.TrimSpace(cred["settle_seconds"])); err == nil && n > 0 {
		cfg.settle = time.Duration(n) * time.Second
	}
	return cfg, nil
}

// includeMatch: whether a file matches the extension filter (doesn't apply to directories).
func (c watchCfg) includeMatch(path string) bool {
	if len(c.include) == 0 {
		return true
	}
	return c.include[strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))]
}

// ignoredPath: if any path segment below root matches an ignore rule → the whole path is ignored.
func (c watchCfg) ignoredPath(path string) bool {
	rel, err := filepath.Rel(c.root, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return true
	}
	for _, seg := range strings.Split(rel, string(filepath.Separator)) {
		if seg != "." && isIgnoredName(seg, c.ignore) {
			return true
		}
	}
	return false
}

// fsWatcher: a watcher for one directory tree. emit(kind, path, info): kind is one of
// created/changed/deleted (info is nil for deleted).
type fsWatcher struct {
	cfg  watchCfg
	emit func(kind, path string, info os.FileInfo)

	w       *fsnotify.Watcher
	mu      sync.Mutex
	known   map[string]bool // inventory of existing files: distinguishes created/changed; the initial inventory emits no events
	pending map[string]*pendingFile
	dirs    int
}

type pendingFile struct {
	timer *time.Timer
	size  int64 // the size last observed; a mismatch at settle-check time means still being written, so the timer resets
}

func newFSWatcher(cfg watchCfg, emit func(kind, path string, info os.FileInfo)) *fsWatcher {
	return &fsWatcher{cfg: cfg, emit: emit, known: map[string]bool{}, pending: map[string]*pendingFile{}}
}

// addTree: recursively adds watches + inventories existing files. When fire=true,
// existing files in the tree are scheduled as if newly created (the "whole directory
// moved in" case — moving a directory in produces only one Create event for the
// directory itself, with no individual event for its inner files). Returns the first
// watch-add error (e.g. the limit was hit).
func (fw *fsWatcher) addTree(root string, fire bool) error {
	var addErr error
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip a subtree that can't be read, don't let one failure stop the whole walk
		}
		if p != fw.cfg.root && fw.cfg.ignoredPath(p) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if werr := fw.w.Add(p); werr != nil {
				if addErr == nil {
					addErr = fmt.Errorf("加监听失败（%s）: %w", p, werr)
				}
				return nil
			}
			fw.mu.Lock()
			fw.dirs++
			fw.mu.Unlock()
			return nil
		}
		if !fw.cfg.includeMatch(p) {
			return nil
		}
		if fire {
			fw.schedule(p)
		} else {
			fw.mu.Lock()
			fw.known[p] = true
			fw.mu.Unlock()
		}
		return nil
	})
	return addErr
}

// schedule: schedules a settle check for a file (both Create/Write go through here; scheduling again just resets the timer).
func (fw *fsWatcher) schedule(path string) {
	var size int64 = -1
	if st, err := os.Stat(path); err == nil {
		size = st.Size()
	}
	fw.mu.Lock()
	defer fw.mu.Unlock()
	if p, ok := fw.pending[path]; ok {
		p.size = size
		p.timer.Reset(fw.cfg.settle)
		return
	}
	p := &pendingFile{size: size}
	p.timer = time.AfterFunc(fw.cfg.settle, func() { fw.settleCheck(path) })
	fw.pending[path] = p
}

// settleCheck: the settle check: if size is still changing after the quiet period, keep
// waiting; once stable, emit created/changed depending on whether it's already known.
func (fw *fsWatcher) settleCheck(path string) {
	st, err := os.Stat(path)
	fw.mu.Lock()
	p, ok := fw.pending[path]
	if !ok {
		fw.mu.Unlock()
		return
	}
	if err != nil { // deleted/moved away before settling: discard (the Remove branch handles "deleted")
		delete(fw.pending, path)
		fw.mu.Unlock()
		return
	}
	if st.Size() != p.size { // still being written: record the new size and wait another round
		p.size = st.Size()
		p.timer.Reset(fw.cfg.settle)
		fw.mu.Unlock()
		return
	}
	kind := "changed"
	if !fw.known[path] {
		kind = "created"
		fw.known[path] = true
	}
	delete(fw.pending, path)
	fw.mu.Unlock()
	fw.emit(kind, path, st)
}

// removed: handles Remove/Rename (moving out of the tree counts as a delete): the file itself + if it's a directory, all known files beneath it.
func (fw *fsWatcher) removed(path string) {
	prefix := path + string(filepath.Separator)
	fw.mu.Lock()
	var gone []string
	if fw.known[path] {
		delete(fw.known, path)
		gone = append(gone, path)
	}
	for k := range fw.known { // a moved/deleted directory doesn't get an individual Remove event for each inner file
		if strings.HasPrefix(k, prefix) {
			delete(fw.known, k)
			gone = append(gone, k)
		}
	}
	for k, p := range fw.pending {
		if k == path || strings.HasPrefix(k, prefix) {
			p.timer.Stop()
			delete(fw.pending, k)
		}
	}
	fw.mu.Unlock()
	for _, g := range gone {
		fw.emit("deleted", g, nil)
	}
}

// run: the main loop. Exits as soon as ctx is canceled (credential removed/changed).
func (fw *fsWatcher) run(ctx context.Context) error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("创建 fsnotify watcher 失败: %w", err)
	}
	fw.w = w
	defer w.Close()
	if err := fw.addTree(fw.cfg.root, false); err != nil {
		return err // typically: the inotify watch limit (DSM defaults to 8192) -- the caller surfaces it on the status board
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-w.Events:
			if !ok {
				return nil
			}
			fw.handle(ev)
		case werr, ok := <-w.Errors:
			if !ok {
				return nil
			}
			log.Printf("[synology] fsnotify 错误: %v", werr)
		}
	}
}

func (fw *fsWatcher) handle(ev fsnotify.Event) {
	path := filepath.Clean(ev.Name)
	if fw.cfg.ignoredPath(path) {
		return
	}
	switch {
	case ev.Op.Has(fsnotify.Create):
		st, err := os.Stat(path)
		if err != nil {
			return // created then immediately gone (a temp file): ignore
		}
		if st.IsDir() {
			if aerr := fw.addTree(path, true); aerr != nil { // add a watch on the new directory; for a directory moved in, backfill created for its inner files
				log.Printf("[synology] %v", aerr)
			}
			return
		}
		if fw.cfg.includeMatch(path) {
			fw.schedule(path)
		}
	case ev.Op.Has(fsnotify.Write):
		if fw.cfg.includeMatch(path) {
			fw.schedule(path)
		}
	case ev.Op.Has(fsnotify.Remove) || ev.Op.Has(fsnotify.Rename):
		fw.removed(path) // rename = moved out of its original spot; if moved elsewhere in the tree, that spot gets a Create
	}
}

// —— source entry point (SDK per-credential instance) ——

func runWatchSource(ctx plugin.SourceCtx) error {
	cfg, err := parseWatchCfg(ctx.Credential())
	if err != nil {
		ctx.ReportStatus("error", err.Error())
		<-ctx.Done()
		return nil
	}
	if st, serr := os.Stat(cfg.root); serr != nil || !st.IsDir() {
		ctx.ReportStatus("error", fmt.Sprintf("监听目录不可用：%s（确认已把 NAS 卷挂载进容器且路径正确）", cfg.root))
		<-ctx.Done()
		return nil
	}

	emit := func(kind, path string, info os.FileInfo) {
		name := filepath.Base(path)
		ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
		dir := filepath.Dir(path)
		// Both the event name and the payload fields are pinned down by the generated
		// TriggerXxx functions -- previously this was a string concatenation + an
		// untyped payload, where a mistake only surfaced at runtime (and as the
		// hard-to-diagnose symptom of "the event just doesn't fire").
		event := "file_" + kind
		var eventID string
		var terr error
		switch kind {
		case "deleted":
			eventID = fmt.Sprintf("%s|del|%d", path, time.Now().UnixNano())
			terr = TriggerFileDeleted(ctx, eventID, &FileDeletedEvent{Path: path, Name: name, Ext: ext, Dir: dir})
		case "created":
			eventID = fmt.Sprintf("%s|%s|%d|%d", path, kind, info.Size(), info.ModTime().UnixNano())
			terr = TriggerFileCreated(ctx, eventID, &FileCreatedEvent{Path: path, Name: name, Ext: ext, Dir: dir,
				Size: int(info.Size()), Mtime: info.ModTime().Format(time.RFC3339)})
		default:
			eventID = fmt.Sprintf("%s|%s|%d|%d", path, kind, info.Size(), info.ModTime().UnixNano())
			terr = TriggerFileChanged(ctx, eventID, &FileChangedEvent{Path: path, Name: name, Ext: ext, Dir: dir,
				Size: int(info.Size()), Mtime: info.ModTime().Format(time.RFC3339)})
		}
		if terr != nil {
			log.Printf("[synology] 推送事件失败（%s %s）: %v", event, path, terr)
		} else {
			log.Printf("[synology] %s %s", event, path)
		}
	}

	fw := newFSWatcher(cfg, emit)
	done := make(chan error, 1)
	go func() { done <- fw.run(ctx) }()

	// Once startup succeeds, report "running" (with the watch scale, readable at a glance
	// on the dashboard); on failure (typically the inotify limit), surface "error" pointing to the fix.
	time.Sleep(300 * time.Millisecond)
	fw.mu.Lock()
	dirs := fw.dirs
	fw.mu.Unlock()
	ctx.ReportStatus("running", fmt.Sprintf("监听 %s（%d 个目录，落定 %s）", cfg.root, dirs, cfg.settle))
	log.Printf("[synology] 开始监听 %s（%d 个目录）", cfg.root, dirs)

	if rerr := <-done; rerr != nil {
		msg := rerr.Error()
		if strings.Contains(msg, "no space") || strings.Contains(msg, "too many") {
			msg += "——inotify watch 上限打满：在 DSM 任务计划以 root 跑 sysctl fs.inotify.max_user_watches=1048576（见插件 README）"
		}
		ctx.ReportStatus("error", msg)
		return rerr
	}
	return nil
}
