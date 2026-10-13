package main

// DSM Web API client: login → FileStation upload → logout. One session per call: uploads are occasional, and a
// cached session id would need expiry handling (DSM drops idle sessions and a duplicate login can end one) for no
// real gain.

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type dsmClient struct {
	base string // https://nas:5001, no trailing slash
	http *http.Client
}

func newDSMClient(rawURL string, insecureTLS bool) (*dsmClient, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("DSM 地址 %q 不对：要带协议和端口，如 https://nas.example.com:5001", rawURL)
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if insecureTLS {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // opted in per credential, for a LAN DSM's self-signed certificate
	}
	// No overall client timeout: an upload of a large file legitimately takes long; the call's ctx bounds it.
	return &dsmClient{base: strings.TrimRight(u.Scheme+"://"+u.Host+u.Path, "/"), http: &http.Client{Transport: tr}}, nil
}

// dsmResp is the envelope of every DSM Web API answer.
type dsmResp struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data"`
	Error   *struct {
		Code int `json:"code"`
	} `json:"error"`
}

// call sends a request and decodes the envelope; a non-2xx status, a body that is not the envelope, or success=false
// is an error (errText names the code for the API in question).
func (c *dsmClient) call(req *http.Request, errText func(int) string) (json.RawMessage, error) {
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("连不上 DSM（%s）: %w", c.base, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("DSM 返回 HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var r dsmResp
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("DSM 的回应不是预期的格式（地址是否指向 DSM？）: %.200s", body)
	}
	if !r.Success {
		code := 0
		if r.Error != nil {
			code = r.Error.Code
		}
		return nil, fmt.Errorf("%s（错误码 %d）", errText(code), code)
	}
	return r.Data, nil
}

// login opens a FileStation session and returns its sid. Auth API version 6 is accepted by both DSM 6.x and 7.x.
func (c *dsmClient) login(ctx context.Context, user, pass string) (string, error) {
	q := url.Values{"api": {"SYNO.API.Auth"}, "version": {"6"}, "method": {"login"}, "account": {user},
		"passwd": {pass}, "session": {"FileStation"}, "format": {"sid"}}
	// POST with a form body: in a GET the password would land in DSM's and any proxy's access logs.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/webapi/auth.cgi", strings.NewReader(q.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	data, err := c.call(req, authErrText)
	if err != nil {
		return "", fmt.Errorf("登录 DSM 失败: %w", err)
	}
	var d struct {
		SID string `json:"sid"`
	}
	if json.Unmarshal(data, &d) != nil || d.SID == "" {
		return "", fmt.Errorf("登录 DSM 失败: 回应里没有会话 id")
	}
	return d.SID, nil
}

// logout ends the session; best effort (the session expires on its own anyway).
func (c *dsmClient) logout(sid string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	q := url.Values{"api": {"SYNO.API.Auth"}, "version": {"6"}, "method": {"logout"}, "session": {"FileStation"}, "_sid": {sid}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/webapi/auth.cgi?"+q.Encode(), nil)
	if err != nil {
		return
	}
	if resp, err := c.http.Do(req); err == nil {
		resp.Body.Close()
	}
}

// upload sends one file into dir. onConflict: error / overwrite / skip. Returns whether DSM skipped it (skip mode
// and a file of that name already there).
func (c *dsmClient) upload(ctx context.Context, sid, dir, name string, data []byte, onConflict string, createParents bool) (bool, error) {
	// The body is written while it is sent (a pipe) instead of being assembled first: data is already a whole copy of
	// the file in memory, a second one in a buffer would double the peak for large files.
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		// DSM reads the fields before the file: the file part must come last.
		fields := [][2]string{{"path", dir}, {"create_parents", fmt.Sprint(createParents)}}
		switch onConflict {
		case "overwrite":
			fields = append(fields, [2]string{"overwrite", "true"})
		case "skip":
			fields = append(fields, [2]string{"overwrite", "false"})
		} // error: leave overwrite out — DSM then refuses an existing name (414 on a DSM 7 tried 2026-10-13; the guide says 1805)
		for _, f := range fields {
			if err := mw.WriteField(f[0], f[1]); err != nil {
				pw.CloseWithError(err)
				return
			}
		}
		fw, err := mw.CreateFormFile("file", name)
		if err == nil {
			_, err = fw.Write(data)
		}
		if err == nil {
			err = mw.Close()
		}
		pw.CloseWithError(err)
	}()
	q := url.Values{"api": {"SYNO.FileStation.Upload"}, "version": {"2"}, "method": {"upload"}, "_sid": {sid}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/webapi/entry.cgi?"+q.Encode(), pr)
	if err != nil {
		pr.Close()
		return false, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	out, err := c.call(req, uploadErrText)
	if err != nil {
		return false, fmt.Errorf("上传 %s 失败: %w", name, err)
	}
	var d struct {
		Skip bool `json:"blSkip"`
	}
	_ = json.Unmarshal(out, &d) // data is optional in the answer; absent = uploaded
	return d.Skip, nil
}

// Error codes from Synology's DSM Login Web API guide and File Station API guide.

func commonErrText(code int) (string, bool) {
	m := map[int]string{
		100: "未知错误", 101: "缺少参数", 102: "DSM 没有这个接口", 103: "接口没有这个方法",
		104: "DSM 不支持这个接口版本", 105: "账号没有权限", 106: "会话超时", 107: "会话被重复登录挤掉", 119: "会话无效",
	}
	s, ok := m[code]
	return s, ok
}

func authErrText(code int) string {
	if s, ok := commonErrText(code); ok {
		return s
	}
	m := map[int]string{
		400: "账号或密码不对", 401: "账号已停用", 402: "账号没有权限", 403: "账号开了两步验证（接口登录无法输入验证码，请换一个没开两步验证的账号）",
		404: "两步验证失败", 406: "DSM 强制要求两步验证（请为这个账号关闭，或换一个账号）", 407: "这个 IP 已被 DSM 封锁（登录失败次数过多）",
		408: "密码已过期，需先在 DSM 里改密码", 409: "密码已过期，需先在 DSM 里改密码", 410: "密码需要重设，需先在 DSM 里改密码",
	}
	if s, ok := m[code]; ok {
		return s
	}
	return "登录被拒绝"
}

func uploadErrText(code int) string {
	if s, ok := commonErrText(code); ok {
		return s
	}
	m := map[int]string{
		400: "文件操作参数不对", 401: "文件操作出现未知错误", 402: "NAS 正忙", 403: "账号不允许做这个文件操作",
		407: "操作不被允许", 408: "目标文件夹不存在（可打开「自动创建文件夹」）", 409: "文件系统不支持", 411: "目标是只读文件系统",
		412: "文件名太长", 413: "文件名太长（加密共享文件夹的上限更短）", 414: "目标位置已有同名文件或文件夹（可把「同名文件」改为覆盖或跳过）",
		415: "超出磁盘配额", 416: "NAS 磁盘空间不足", 417: "NAS 读写出错", 418: "路径不合法（要以共享文件夹开头，如 /research/...）",
		419: "文件名不合法", 420: "文件名不符合 FAT 规则", 421: "设备忙",
		1800: "上传被中断（请求不完整）", 1801: "上传等待超时", 1802: "缺少文件名", 1803: "上传连接被取消",
		1804: "文件太大，FAT 文件系统存不下", 1805: "目标文件夹已有同名文件（可把「同名文件」改为覆盖或跳过）",
	}
	if s, ok := m[code]; ok {
		return s
	}
	return "上传被拒绝"
}
