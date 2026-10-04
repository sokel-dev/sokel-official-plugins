package main

// 仓库域：列仓库/读写文件/分支/提交。

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/sokel-dev/sokel-official-plugins/github/schema"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
)

func opReposList(ctx plugin.Ctx, in *ReposListIn) (*ReposListOut, error) {
	q := perPage(in.Page)
	q["sort"] = orDefault(in.Sort, "updated")
	path := "/user/repos"
	if owner := strings.TrimSpace(in.Owner); owner != "" {
		// 用户与组织的接口不同，但 /users/{o}/repos 对组织也有效（只是拿不到私有仓库），
		// 先试组织，404 再退回用户——反过来的话组织的私有仓库会静默漏掉。
		path = "/orgs/" + owner + "/repos"
		q["type"] = orDefault(in.Type, "all")
		raw, h, err := ghCall(ctx, http.MethodGet, path, q)
		if err == nil {
			return reposOut(raw, h), nil
		}
		path = "/users/" + owner + "/repos"
		delete(q, "type") // 用户接口的 type 取值集合不同，交给默认
	} else {
		q["type"] = orDefault(in.Type, "all")
	}
	raw, h, err := ghCall(ctx, http.MethodGet, path, q)
	if err != nil {
		return nil, err
	}
	return reposOut(raw, h), nil
}

func reposOut(raw []byte, h http.Header) *ReposListOut {
	list := digList(raw)
	out := &ReposListOut{Count: len(list), HasMore: hasNext(h)}
	for _, m := range list {
		out.Repos = append(out.Repos, toRepo(m))
	}
	return out
}

func toRepo(m map[string]any) schema.Repo {
	return schema.Repo{
		FullName:      str(m, "full_name"),
		Name:          str(m, "name"),
		Owner:         nested(m, "owner", "login"),
		Description:   str(m, "description"),
		Private:       boolean(m, "private"),
		Fork:          boolean(m, "fork"),
		Archived:      boolean(m, "archived"),
		DefaultBranch: str(m, "default_branch"),
		Stars:         num(m, "stargazers_count"),
		OpenIssues:    num(m, "open_issues_count"),
		URL:           str(m, "html_url"),
		UpdatedAt:     str(m, "updated_at"),
	}
}

func opRepoGet(ctx plugin.Ctx, in *RepoGetIn) (*RepoGetOut, error) {
	rp, err := repoPath(in.Repo)
	if err != nil {
		return nil, err
	}
	raw, _, err := ghCall(ctx, http.MethodGet, rp, nil)
	if err != nil {
		return nil, err
	}
	m := digObj(raw)
	r := toRepo(m)
	return &RepoGetOut{Repo: r, Topics: namesOf(m["topics"], "")}, nil
}

func opFileGet(ctx plugin.Ctx, in *FileGetIn) (*FileGetOut, error) {
	rp, err := repoPath(in.Repo)
	if err != nil {
		return nil, err
	}
	q := map[string]any{}
	if r := strings.TrimSpace(in.Ref); r != "" {
		q["ref"] = r
	}
	raw, _, err := ghCall(ctx, http.MethodGet, rp+"/contents/"+pathEscape(in.Path), q)
	if err != nil {
		return nil, err
	}
	m := digObj(raw)
	// 目录会返回数组而不是对象——digObj 得到 nil，这时给一句能看懂的话。
	if m == nil {
		return nil, fmt.Errorf("%q 是个目录不是文件——要列目录内容请用「通用调用」打 %s/contents/%s",
			in.Path, rp, in.Path)
	}
	content := ""
	if enc := str(m, "encoding"); enc == "base64" {
		// GitHub 的 base64 带换行，标准解码器不认——先去掉。
		b, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(str(m, "content"), "\n", ""))
		if err != nil {
			return nil, fmt.Errorf("解码文件内容失败: %w", err)
		}
		content = string(b)
	} else {
		content = str(m, "content")
	}
	return &FileGetOut{
		Content: content, SHA: str(m, "sha"),
		Size: num(m, "size"), URL: str(m, "html_url"),
	}, nil
}

// opFileWrite 写文件。
//
// Contents API 更新已有文件时**必须带 blob sha**，不带会 422，而错误文字不会说
// 「你少给了 sha」。所以这里先探一次：存在就带 sha 走更新，不存在就走新建。
// 这一趟额外请求换掉的是一整类让人摸不着头脑的失败。
func opFileWrite(ctx plugin.Ctx, in *FileWriteIn) (*FileWriteOut, error) {
	rp, err := repoPath(in.Repo)
	if err != nil {
		return nil, err
	}
	branch := strings.TrimSpace(in.Branch)
	body := map[string]any{
		"message": in.Message,
		"content": base64.StdEncoding.EncodeToString([]byte(in.Content)),
	}
	if branch != "" {
		body["branch"] = branch
	}
	// 探已有文件。404 = 新建，其余错误照常抛（别把「没权限」也当成新建）。
	q := map[string]any{}
	if branch != "" {
		q["ref"] = branch
	}
	created := true
	if raw, _, err := ghCall(ctx, http.MethodGet, rp+"/contents/"+pathEscape(in.Path), q); err == nil {
		if m := digObj(raw); m != nil {
			if sha := str(m, "sha"); sha != "" {
				body["sha"] = sha
				created = false
			}
		}
	} else if !strings.Contains(err.Error(), "404") {
		return nil, err
	}
	raw, _, err := ghCall(ctx, http.MethodPut, rp+"/contents/"+pathEscape(in.Path), body)
	if err != nil {
		return nil, err
	}
	m := digObj(raw)
	return &FileWriteOut{
		CommitSHA: nested(m, "commit", "sha"),
		SHA:       nested(m, "content", "sha"),
		Created:   created,
		URL:       nested(m, "content", "html_url"),
	}, nil
}

// opBranchCreate 建分支。已存在时**不报错**——机器人重跑一次不该炸。
func opBranchCreate(ctx plugin.Ctx, in *BranchCreateIn) (*BranchCreateOut, error) {
	rp, err := repoPath(in.Repo)
	if err != nil {
		return nil, err
	}
	branch := strings.TrimPrefix(strings.TrimSpace(in.Branch), "refs/heads/")
	if branch == "" {
		return nil, fmt.Errorf("新分支名是空的")
	}
	// 已存在就直接回，幂等。
	if raw, _, err := ghCall(ctx, http.MethodGet, rp+"/git/ref/heads/"+pathEscape(branch), nil); err == nil {
		return &BranchCreateOut{Branch: branch, SHA: nested(digObj(raw), "object", "sha"), Existed: true}, nil
	}
	sha, err := resolveRef(ctx, rp, in.From)
	if err != nil {
		return nil, err
	}
	raw, _, err := ghCall(ctx, http.MethodPost, rp+"/git/refs", map[string]any{
		"ref": "refs/heads/" + branch, "sha": sha,
	})
	if err != nil {
		return nil, err
	}
	return &BranchCreateOut{Branch: branch, SHA: nested(digObj(raw), "object", "sha")}, nil
}

// resolveRef 把「分支名或 sha 或空」解析成一个提交 sha。
func resolveRef(ctx plugin.Ctx, rp, from string) (string, error) {
	f := strings.TrimSpace(from)
	if f == "" {
		raw, _, err := ghCall(ctx, http.MethodGet, rp, nil)
		if err != nil {
			return "", err
		}
		f = str(digObj(raw), "default_branch")
	}
	// 先当分支解；解不出再当 sha 用（40 位十六进制）。
	if raw, _, err := ghCall(ctx, http.MethodGet, rp+"/git/ref/heads/"+pathEscape(f), nil); err == nil {
		return nested(digObj(raw), "object", "sha"), nil
	}
	if len(f) >= 7 && isHex(f) {
		return f, nil
	}
	return "", fmt.Errorf("找不到分支或提交 %q——分支名要不带 refs/heads/，提交要给完整 sha", from)
}

func isHex(s string) bool {
	for _, c := range s {
		if !strings.ContainsRune("0123456789abcdefABCDEF", c) {
			return false
		}
	}
	return true
}

func opBranchesList(ctx plugin.Ctx, in *BranchesListIn) (*BranchesListOut, error) {
	rp, err := repoPath(in.Repo)
	if err != nil {
		return nil, err
	}
	raw, h, err := ghCall(ctx, http.MethodGet, rp+"/branches", perPage(in.Page))
	if err != nil {
		return nil, err
	}
	list := digList(raw)
	out := &BranchesListOut{Count: len(list), HasMore: hasNext(h)}
	for _, m := range list {
		out.Branches = append(out.Branches, schema.Branch{
			Name: str(m, "name"), SHA: nested(m, "commit", "sha"),
			Protected: boolean(m, "protected"),
		})
	}
	return out, nil
}

func opCommitsList(ctx plugin.Ctx, in *CommitsListIn) (*CommitsListOut, error) {
	rp, err := repoPath(in.Repo)
	if err != nil {
		return nil, err
	}
	q := perPage(in.Page)
	putIf(q, "sha", in.Branch)
	putIf(q, "path", in.Path)
	putIf(q, "since", in.Since)
	raw, h, err := ghCall(ctx, http.MethodGet, rp+"/commits", q)
	if err != nil {
		return nil, err
	}
	list := digList(raw)
	out := &CommitsListOut{Count: len(list), HasMore: hasNext(h)}
	for _, m := range list {
		c := obj(m, "commit")
		out.Commits = append(out.Commits, schema.Commit{
			SHA: str(m, "sha"), Message: clip(str(c, "message"), 2000),
			Author: nested(c, "author", "name"), Date: nested(c, "author", "date"),
			URL: str(m, "html_url"),
		})
	}
	return out, nil
}

// —— 小工具 ——

func orDefault(v, def string) string {
	if s := strings.TrimSpace(v); s != "" {
		return s
	}
	return def
}

// putIf 非空才放进查询串——空字符串传给 GitHub 有时会被当成有效过滤条件。
func putIf(m map[string]any, k, v string) {
	if s := strings.TrimSpace(v); s != "" {
		m[k] = s
	}
}

// pathEscape 转义路径片段，但**保留斜杠**：文件路径 src/main.go 要原样进 URL。
func pathEscape(p string) string {
	parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(p), "/"), "/")
	for i, s := range parts {
		parts[i] = urlPathEscape(s)
	}
	return strings.Join(parts, "/")
}

func jsonBytes(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}
