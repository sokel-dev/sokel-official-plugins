package main

// 工作树盘点：这台机器上现在有哪些工作树、各自什么状态。
//
// 有它之前，「管理」只能是「你说出 project + branch，我删掉」——看不见就等于管不了：
// 磁盘被谁吃掉、哪些还有没提交的改动删不得、哪个会话还能接管，全靠人记。

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sokel-dev/sokel-official-plugins/claude-code/schema"
)

// scanWorktrees 扫工作区。project 非空则只看那一个项目。
func scanWorktrees(project string) []schema.WorktreeInfo {
	trees := filepath.Join(workspaceRoot(), "trees")
	projDirs, err := os.ReadDir(trees)
	if err != nil {
		return nil // 工作区还没建 = 一个都没有，不是错误
	}
	want := slug(project)
	var out []schema.WorktreeInfo
	for _, pd := range projDirs {
		if !pd.IsDir() || (want != "" && pd.Name() != want) {
			continue
		}
		branches, err := os.ReadDir(filepath.Join(trees, pd.Name()))
		if err != nil {
			continue
		}
		for _, bd := range branches {
			if !bd.IsDir() {
				continue
			}
			out = append(out, inspectTree(filepath.Join(trees, pd.Name(), bd.Name()), pd.Name(), bd.Name()))
		}
	}
	return out
}

func inspectTree(path, projDir, branchDir string) schema.WorktreeInfo {
	info := schema.WorktreeInfo{Path: path, Project: projDir, Branch: branchDir}
	// 分支名从 git 里取真名：目录名是 slug 过的（斜杠换下划线），拿它当分支名会误导。
	if out, err := git(path, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		if b := strings.TrimSpace(out); b != "" && b != "HEAD" {
			info.Branch = b
		}
	}
	info.SizeMB = dirSizeMB(path)
	if st, err := os.Stat(path); err == nil {
		info.LastActive = st.ModTime().Format("2006-01-02 15:04")
		info.IdleDays = int(time.Since(st.ModTime()).Hours() / 24)
	}
	info.Dirty = len(changedFiles(path)) > 0
	info.Sessions, info.LastSession = sessionsFor(path)
	return info
}

// sessionsFor 这个工作目录下有几个 CC 会话、最近一个是哪个。
//
// CC 把会话按 **cwd** 归档：~/.claude/projects/<路径里的 / 换成 ->/<会话id>.jsonl。
// 必须先解符号链接：macOS 上 /tmp 实际是 /private/tmp，CC 记的是解开之后的那个，
// 不解就一个都找不到（实测踩过）。
func sessionsFor(treePath string) (int, string) {
	real, err := filepath.EvalSymlinks(treePath)
	if err != nil {
		real = treePath
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return 0, ""
	}
	dir := filepath.Join(home, ".claude", "projects", strings.ReplaceAll(real, string(os.PathSeparator), "-"))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, ""
	}
	var n int
	var newest time.Time
	var newestID string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		n++
		fi, err := e.Info()
		if err != nil {
			continue
		}
		if fi.ModTime().After(newest) {
			newest, newestID = fi.ModTime(), strings.TrimSuffix(e.Name(), ".jsonl")
		}
	}
	return n, newestID
}

// dirSizeMB 目录占用。走一遍文件树而不是调 du：少一个外部依赖，也不必担心平台差异。
func dirSizeMB(path string) int {
	var total int64
	_ = filepath.WalkDir(path, func(_ string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil // 单个文件读不到就跳过，不因为一个权限问题让整次盘点失败
		}
		if fi, err := d.Info(); err == nil {
			total += fi.Size()
		}
		return nil
	})
	return int(total / (1024 * 1024))
}
