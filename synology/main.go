// synology：NAS 文件变动监听插件（sokel SDK，事件源）。
//
// 部署形态：Docker 跑在 NAS 本机（Synology Container Manager），把卷目录 bind mount 进容器
// （如 /volume1/research:/watch）。inotify 不穿网络文件系统——容器必须与文件所在内核同机，
// 其他电脑经 SMB/AFP 写入时 smbd 在本地落盘，事件照常触发。部署细节（含 Synology 的
// inotify watch 上限任务计划）见 README.md。
//
// 凭证 = 监听配置（一个凭证 = 一个受监听子目录 = 一个 source 实例；多目录=多凭证，
// 平台按凭证分片下发给实例，复用 per-credential source 架构）：
//   - path            容器内子目录（挂载点下），如 /watch/研报入库
//   - include         可选扩展名过滤（逗号分隔，如 pdf,docx；空=全部）
//   - ignore          可选追加忽略模式（逗号分隔子串；内置 Synology 垃圾目录恒忽略）
//   - settle_seconds  落定秒数（SMB 临时文件之舞/大文件拷贝：静默 N 秒且 size 稳定才上报，默认 2）
//
// 事件：file_created / file_changed / file_deleted（各自是画布出口分支；只带元数据不带字节，
// 下游用 read_file 按需取文件）。操作：read_file / list_dir / move_file / delete_file，
// 路径一律 jail 在凭证 path 内（防越界读写挂载卷其它位置）。
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

// —— 事件契约 ——

// —— 操作 ——

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
	// 边读边传：NAS 上是视频、压缩包这类东西，先 ReadFile 进内存的话，
	// 一个 2GB 的文件就等于要 2GB 常驻内存——那不是慢一点，是进程直接被撑爆。
	// 原先为此设了 200MB 上限；流式之后上限没有意义，去掉。
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
		var size int64 // 契约里 DirEntry.Size 是 int64（schema 声明的类型即事实）
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
	// 目标是目录（显式 / 结尾，或已存在的目录）→ 拼原文件名。
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

// —— 路径 jail：一切操作路径限制在凭证监听目录内（防越界读写挂载卷其它位置）——

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

// —— 凭证体检 ——

// healthCheck：看监听目录在不在、读不读得了。
//
// 不可用时返回 ok=false + message 而**不是** error：平台把 error 当「这个插件没法体检」，
// 把 ok=false 当「体检结论是不可用」——后者才是这里要说的话，且 message 里那句系统原文
// （no such file / permission denied）直接指向卷没挂上还是容器用户没权限。
//
// 刻意只读不写：这个目录正被自己的 fswatch 盯着，写个探针文件进去等于凭空触发一次工作流。
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
	RegisterCredential(p)  // 凭证契约（schema 声明生成；Cred 在 zz_credential.go）
	p.SetDoc(usageDoc, "") // 使用说明（docs/*.md）：凭证怎么拿、有什么坑，随握手上报给平台

	OnReadFile(p, readFile)
	OnListDir(p, listDir)
	OnMoveFile(p, moveFile)
	OnDeleteFile(p, deleteFile)
	OnHealthCheck(p, healthCheck)

	DeclareEvents(p)
	sokel.RegisterSource(p, sokel.Source{ID: "fswatch", Label: "文件变动监听"}, runWatchSource)

	log.Fatal(p.Run())
}
