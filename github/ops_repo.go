package main

// Repository domain: list repos / read-write files / branches / commits.

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
		// User and org endpoints differ, but /users/{o}/repos also works for an org (it just
		// can't see private repos) — try org first, then fall back to user on 404. Doing it the
		// other way around would silently drop the org's private repos.
		path = "/orgs/" + owner + "/repos"
		q["type"] = orDefault(in.Type, "all")
		raw, h, err := ghCall(ctx, http.MethodGet, path, q)
		if err == nil {
			return reposOut(raw, h), nil
		}
		path = "/users/" + owner + "/repos"
		delete(q, "type") // the user endpoint's type values differ; leave it to the default
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
	// A directory returns an array instead of an object — digObj ends up nil, so give a readable
	// message here.
	if m == nil {
		return nil, fmt.Errorf("%q 是个目录不是文件——要列目录内容请用「通用调用」打 %s/contents/%s",
			in.Path, rp, in.Path)
	}
	content := ""
	if enc := str(m, "encoding"); enc == "base64" {
		// GitHub's base64 has line breaks that the standard decoder rejects — strip them first.
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

// opFileWrite writes a file.
//
// The Contents API **requires the blob sha** when updating an existing file; omitting it gives a
// 422 whose error text won't say "you forgot the sha". So we probe first: if the file exists,
// include its sha for an update; if not, create it. That extra request trades away a whole class
// of baffling failures.
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
	// Probe for the existing file. 404 means create; any other error is raised as usual (don't
	// treat "no permission" as "create").
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

// opBranchCreate creates a branch. It **does not error** when the branch already exists — a bot
// re-running shouldn't blow up.
func opBranchCreate(ctx plugin.Ctx, in *BranchCreateIn) (*BranchCreateOut, error) {
	rp, err := repoPath(in.Repo)
	if err != nil {
		return nil, err
	}
	branch := strings.TrimPrefix(strings.TrimSpace(in.Branch), "refs/heads/")
	if branch == "" {
		return nil, fmt.Errorf("新分支名是空的")
	}
	// Already exists -> just return it, idempotent.
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

// resolveRef resolves "a branch name, a sha, or empty" into a commit sha.
func resolveRef(ctx plugin.Ctx, rp, from string) (string, error) {
	f := strings.TrimSpace(from)
	if f == "" {
		raw, _, err := ghCall(ctx, http.MethodGet, rp, nil)
		if err != nil {
			return "", err
		}
		f = str(digObj(raw), "default_branch")
	}
	// Try resolving it as a branch first; if that fails, treat it as a sha (40-char hex).
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

// —— helpers ——

func orDefault(v, def string) string {
	if s := strings.TrimSpace(v); s != "" {
		return s
	}
	return def
}

// putIf only adds the value to the query string when it's non-empty — an empty string passed to
// GitHub is sometimes treated as a valid filter condition.
func putIf(m map[string]any, k, v string) {
	if s := strings.TrimSpace(v); s != "" {
		m[k] = s
	}
}

// pathEscape escapes path segments but **keeps slashes**: a file path like src/main.go must go
// into the URL as-is.
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
