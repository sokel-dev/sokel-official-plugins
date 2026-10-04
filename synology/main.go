// synology: a NAS file-change watching plugin (sokel SDK, event source).
//
// Deployment shape: Docker runs on the NAS itself (Synology Container Manager), with the
// volume directory bind-mounted into the container (e.g. /volume1/research:/watch).
// inotify doesn't cross network filesystems — the container must share a kernel with
// wherever the files live; when another computer writes over SMB/AFP, smbd writes to disk
// locally, so the event still fires as usual. See README.md for deployment details
// (including Synology's inotify watch limit and the scheduled task for it).
//
// Credential = watch config (one credential = one watched subdirectory = one source
// instance; multiple directories = multiple credentials, with the platform sharding
// delivery to instances by credential, reusing the per-credential source architecture):
//   - path            the subdirectory inside the container (under the mount point), e.g. /watch/research-intake
//   - include         optional extension filter (comma-separated, e.g. pdf,docx; empty = all)
//   - ignore          optional extra ignore patterns (comma-separated substrings; built-in Synology junk directories are always ignored)
//   - settle_seconds  settle time in seconds (for the SMB temp-file dance / large file copies: only reported once N seconds of silence pass and size has stabilized; default 2)
//
// Events: file_created / file_changed / file_deleted (each its own branch on the canvas;
// carries only metadata, not bytes — downstream uses read_file to fetch the file as
// needed). Operations: read_file / list_dir / move_file / delete_file, with paths always
// jailed inside the credential's path (to prevent reading/writing outside the mounted
// volume).
package main

//go:generate go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen

import (
	"fmt"
	"log"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sokel-dev/sokel-official-plugins/synology/schema"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
)

// —— event contract ——

// —— operations ——

func readFile(ctx plugin.Ctx, in *ReadFileIn) (*ReadFileOut, error) {
	p, err := jailPath(ctx, in.Path)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(p)
	if err != nil {
		return nil, fmt.Errorf("读取文件信息失败: %w", err)
	}
	if st.IsDir() {
		return nil, fmt.Errorf("%s 是目录，不是文件", in.Path)
	}
	mt := mime.TypeByExtension(filepath.Ext(p))
	if mt == "" {
		mt = "application/octet-stream"
	}
	// Stream read-to-upload: NAS files are often videos, archives, that kind of thing — if
	// ReadFile loaded one into memory first, a 2GB file would mean 2GB resident memory,
	// which isn't just slower, it blows up the process outright. There used to be a
	// 200MB cap for this reason; once streaming was in place the cap became meaningless
	// and was removed.
	rf, err := os.Open(p)
	if err != nil {
		return nil, fmt.Errorf("打开文件失败: %w", err)
	}
	defer rf.Close()
	f, err := ctx.UploadReader(filepath.Base(p), mt, rf)
	if err != nil {
		return nil, fmt.Errorf("上传平台文件层失败: %w", err)
	}
	return &ReadFileOut{File: f, Name: filepath.Base(p), Size: int(st.Size()), Mtime: st.ModTime().Format(time.RFC3339)}, nil
}

func listDir(ctx plugin.Ctx, in *ListDirIn) (*ListDirOut, error) {
	p, err := jailPath(ctx, orDot(in.Dir))
	if err != nil {
		return nil, err
	}
	ents, err := os.ReadDir(p)
	if err != nil {
		return nil, fmt.Errorf("读取目录失败: %w", err)
	}
	res := []schema.DirEntry{}
	for _, e := range ents {
		if isIgnoredName(e.Name(), nil) {
			continue
		}
		info, ierr := e.Info()
		var size int64 // DirEntry.Size is int64 in the contract (the type schema declares is the source of truth)
		mtime := ""
		if ierr == nil {
			size = info.Size()
			mtime = info.ModTime().Format(time.RFC3339)
		}
		res = append(res, schema.DirEntry{Name: e.Name(), Path: filepath.Join(p, e.Name()), IsDir: e.IsDir(), Size: size, Mtime: mtime})
	}
	return &ListDirOut{Entries: res, Count: len(res)}, nil
}

func moveFile(ctx plugin.Ctx, in *MoveFileIn) (*MoveFileOut, error) {
	src, err := jailPath(ctx, in.Path)
	if err != nil {
		return nil, err
	}
	dst, err := jailPath(ctx, in.To)
	if err != nil {
		return nil, err
	}
	// Target is a directory (explicit trailing /, or an existing directory) → append the original file name.
	if strings.HasSuffix(in.To, "/") {
		dst = filepath.Join(dst, filepath.Base(src))
	} else if st, serr := os.Stat(dst); serr == nil && st.IsDir() {
		dst = filepath.Join(dst, filepath.Base(src))
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return nil, fmt.Errorf("创建目标目录失败: %w", err)
	}
	if err := os.Rename(src, dst); err != nil {
		return nil, fmt.Errorf("移动失败: %w", err)
	}
	return &MoveFileOut{OK: true, To: dst}, nil
}

func deleteFile(ctx plugin.Ctx, in *DeleteFileIn) (*DeleteFileOut, error) {
	p, err := jailPath(ctx, in.Path)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(p)
	if err != nil {
		return nil, fmt.Errorf("读取文件信息失败: %w", err)
	}
	if st.IsDir() {
		return nil, fmt.Errorf("拒绝删除目录：%s（本操作只删文件）", in.Path)
	}
	if err := os.Remove(p); err != nil {
		return nil, fmt.Errorf("删除失败: %w", err)
	}
	return &DeleteFileOut{OK: true}, nil
}

// —— path jail: every operation path is confined to the credential's watched directory (to prevent reading/writing outside the mounted volume) ——

func orDot(p string) string {
	if strings.TrimSpace(p) == "" {
		return "."
	}
	return p
}

func jailPath(ctx sokel.Ctx, p string) (string, error) {
	cred := sokel.CredentialAs[Cred](ctx)
	root := strings.TrimSpace(cred.Path)
	if root == "" {
		return "", fmt.Errorf("本次调用未带凭证或凭证未配置监听目录 path（操作路径以它为界）")
	}
	return jailTo(filepath.Clean(root), p)
}

func jailTo(root, p string) (string, error) {
	if strings.TrimSpace(p) == "" {
		return "", fmt.Errorf("path 为空")
	}
	abs := p
	if !filepath.IsAbs(p) {
		abs = filepath.Join(root, p)
	}
	abs = filepath.Clean(abs)
	if abs != root && !strings.HasPrefix(abs, root+string(filepath.Separator)) {
		return "", fmt.Errorf("路径越界：%s 不在监听目录 %s 内", p, root)
	}
	return abs, nil
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// —— credential health check ——

// healthCheck: checks whether the watched directory exists and is readable.
//
// Returns ok=false + message when unavailable, **not** an error: the platform treats an
// error as "this plugin's health check itself failed", and ok=false as "the health check's
// conclusion is unavailable" — the latter is what needs to be said here, and the raw
// system message in message (no such file / permission denied) points straight at
// whether the volume isn't mounted or the container user lacks permission.
//
// Deliberately read-only, no writes: this directory is being watched by its own fswatch,
// so writing a probe file into it would trigger a workflow out of nowhere.
func healthCheck(ctx plugin.Ctx, _ *HealthCheckIn) (*HealthCheckOut, error) {
	cfg, err := parseWatchCfg(ctx.Credential())
	if err != nil {
		return &HealthCheckOut{Message: err.Error()}, nil
	}
	st, err := os.Stat(cfg.root)
	if err != nil {
		return &HealthCheckOut{Path: cfg.root, Message: fmt.Sprintf(
			"监听目录不可访问: %v（对照 docker 的 -v：凭证填的是**容器内**路径）", err)}, nil
	}
	if !st.IsDir() {
		return &HealthCheckOut{Path: cfg.root, Message: cfg.root + " 不是目录"}, nil
	}
	ents, err := os.ReadDir(cfg.root)
	if err != nil {
		return &HealthCheckOut{Path: cfg.root, Message: fmt.Sprintf(
			"目录读不了: %v（容器用户对挂载卷没有读权限）", err)}, nil
	}
	n := 0
	for _, e := range ents {
		if !isIgnoredName(e.Name(), cfg.ignore) {
			n++
		}
	}
	return &HealthCheckOut{OK: true, Path: cfg.root, Entries: n,
		Message: fmt.Sprintf("监听目录就绪（%s，第一层 %d 项）", cfg.root, n)}, nil
}

func main() {
	token := sokel.Env("TOKEN")
	if token == "" {
		log.Fatal("请设置 SOKEL_TOKEN（插件管理里该插件接入组的 token）")
	}
	p := sokel.New(sokel.Config{
		Endpoint: env("SOKEL_ENDPOINT", "http://localhost:8088"),
		Token:    token,
		Name:     "synology",
	})
	RegisterCredential(p)  // credential contract (generated from the schema declaration; Cred is in zz_credential.go)
	p.SetDoc(usageDoc, "") // usage doc (docs/*.md): how to get the credential, what the gotchas are; reported to the platform during the handshake

	OnReadFile(p, readFile)
	OnListDir(p, listDir)
	OnMoveFile(p, moveFile)
	OnDeleteFile(p, deleteFile)
	OnHealthCheck(p, healthCheck)

	DeclareEvents(p)
	sokel.RegisterSource(p, sokel.Source{ID: "fswatch", Label: "文件变动监听"}, runWatchSource)

	log.Fatal(p.Run())
}
