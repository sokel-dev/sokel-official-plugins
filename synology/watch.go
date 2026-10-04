// 文件变动监听 source（per-credential：一个凭证 = 一个受监听子目录 = 本文件跑一个实例）。
//
// 设计要点（Synology/SMB 实况驱动，见 README）：
//   - fsnotify 不递归：初始 walk 全树加 watch，新建子目录动态补；整目录搬入时给内部文件补 created。
//   - 落定检测：SMB 客户端保存 = 临时文件+rename，大文件拷贝 = create+持续 write——
//     文件静默 settle 秒且 size 稳定才上报，否则 wf 会拿到写了一半的文件。
//   - Synology 垃圾恒忽略：@eaDir（缩略图）/#recycle（回收站）/@* 系统目录/Office 锁文件/._* AppleDouble。
//   - inotify watch 上限（DSM 默认 8192）打满时 Add 报 no space：状态板亮 error 指去调 sysctl，不静默。
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

// 内置忽略（Synology/系统噪声）：目录名或文件名精确/前缀命中即忽略整棵子树。
var builtinIgnoreExact = map[string]bool{
	"#recycle": true, "#snapshot": true, ".SynologyWorkingDirectory": true,
	"Thumbs.db": true, ".DS_Store": true, "desktop.ini": true,
}
var builtinIgnorePrefix = []string{"@", "._", "~$"} // @eaDir/@tmp 等系统目录、AppleDouble、Office 锁
var builtinIgnoreSuffix = []string{".tmp", ".part", ".crdownload", ".swp", ".download"}

// isIgnoredName 名字级忽略：内置规则 + 凭证追加的子串模式。
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
	include map[string]bool // 小写扩展名（无点）；空=全部
	ignore  []string        // 追加忽略子串
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

// includeMatch 文件是否命中扩展名过滤（目录不适用）。
func (c watchCfg) includeMatch(path string) bool {
	if len(c.include) == 0 {
		return true
	}
	return c.include[strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))]
}

// ignoredPath root 以下任一路径段命中忽略规则 → 整条路径忽略。
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

// fsWatcher 一棵目录树的监听器。emit(kind, path, info)：kind ∈ created/changed/deleted（deleted 时 info=nil）。
type fsWatcher struct {
	cfg  watchCfg
	emit func(kind, path string, info os.FileInfo)

	w       *fsnotify.Watcher
	mu      sync.Mutex
	known   map[string]bool // 已存在文件清单：区分 created/changed；初始盘点不发事件
	pending map[string]*pendingFile
	dirs    int
}

type pendingFile struct {
	timer *time.Timer
	size  int64 // 上次观察到的 size；落定检查时不一致 → 还在写，重新计时
}

func newFSWatcher(cfg watchCfg, emit func(kind, path string, info os.FileInfo)) *fsWatcher {
	return &fsWatcher{cfg: cfg, emit: emit, known: map[string]bool{}, pending: map[string]*pendingFile{}}
}

// addTree 递归加 watch + 盘点既有文件。fire=true 时对树内既有文件按新增调度（整目录搬入场景——
// 目录 move 进来只有目录一条 Create 事件，内部文件不会逐个报）。返回首个 watch 失败错误（上限打满）。
func (fw *fsWatcher) addTree(root string, fire bool) error {
	var addErr error
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // 读不动的子树跳过，不因单点失败中断整体
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

// schedule 对一个文件安排落定检查（Create/Write 都走这里；重复调度=重置计时）。
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

// settleCheck 落定检查：静默期后 size 仍在变 → 继续等；稳定 → 按 known 与否发 created/changed。
func (fw *fsWatcher) settleCheck(path string) {
	st, err := os.Stat(path)
	fw.mu.Lock()
	p, ok := fw.pending[path]
	if !ok {
		fw.mu.Unlock()
		return
	}
	if err != nil { // 落定前被删/移走：丢弃（Remove 分支负责 deleted）
		delete(fw.pending, path)
		fw.mu.Unlock()
		return
	}
	if st.Size() != p.size { // 还在写：记录新 size，再等一轮
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

// removed 处理 Remove/Rename（移出树同删）：文件本体 + 若是目录则其下全部已知文件。
func (fw *fsWatcher) removed(path string) {
	prefix := path + string(filepath.Separator)
	fw.mu.Lock()
	var gone []string
	if fw.known[path] {
		delete(fw.known, path)
		gone = append(gone, path)
	}
	for k := range fw.known { // 目录被移走/删除：内部文件不会逐个报 Remove
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

// run 主循环。ctx 取消（凭证被移除/变更）即收摊。
func (fw *fsWatcher) run(ctx context.Context) error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("创建 fsnotify watcher 失败: %w", err)
	}
	fw.w = w
	defer w.Close()
	if err := fw.addTree(fw.cfg.root, false); err != nil {
		return err // 典型：inotify watch 上限（DSM 默认 8192）——调用方把它亮到状态板
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
			return // 创建即消失（临时文件）：无视
		}
		if st.IsDir() {
			if aerr := fw.addTree(path, true); aerr != nil { // 新目录补 watch；搬入目录给内部文件补 created
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
		fw.removed(path) // rename=移出原位；若移进树内别处，那边会收到 Create
	}
}

// —— source 入口（SDK per-credential 实例）——

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
		// 事件名与 payload 字段都由生成的 TriggerXxx 定死——此前是拼字符串 +
		// 无类型 payload，写错要等运行期（而且是「事件没触发」这种难查的症状）。
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

	// 起表成功后报 running（带监听规模，面板一眼可读）；失败（典型 inotify 上限）亮 error 指路。
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
