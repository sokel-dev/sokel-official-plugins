package main

// Worktree inventory: which worktrees exist on this machine right now, and what state each is in.
//
// Before this, "management" could only mean "you tell me the project + branch, I delete it" — if you
// can't see it, you can't manage it: who's eating the disk, which ones have uncommitted changes that
// can't be deleted, which session can still be resumed — all of that had to be kept in someone's head.

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sokel-dev/sokel-official-plugins/claude-code/schema"
)

// scanWorktrees scans the workspace. If project is non-empty, only that one project is scanned.
func scanWorktrees(project string) []schema.WorktreeInfo {
	trees := filepath.Join(workspaceRoot(), "trees")
	projDirs, err := os.ReadDir(trees)
	if err != nil {
		return nil // workspace not created yet = there are none, not an error
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
	// Get the real branch name from git: the directory name is slugified (slashes become underscores),
	// so using it as the branch name would be misleading.
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

// sessionsFor reports how many CC sessions exist for this working directory, and which one is most recent.
//
// CC files sessions by **cwd**: ~/.claude/projects/<path with / replaced by ->/<session-id>.jsonl.
// Symlinks must be resolved first: on macOS /tmp is actually /private/tmp, and CC records the resolved
// path, so skipping this step finds zero sessions (hit this in practice).
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

// dirSizeMB computes disk usage of a directory. Walks the file tree instead of shelling out to du: one
// fewer external dependency, and no need to worry about platform differences.
func dirSizeMB(path string) int {
	var total int64
	_ = filepath.WalkDir(path, func(_ string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil // skip a file we can't read — don't let one permission error fail the whole scan
		}
		if fi, err := d.Info(); err == nil {
			total += fi.Size()
		}
		return nil
	})
	return int(total / (1024 * 1024))
}
