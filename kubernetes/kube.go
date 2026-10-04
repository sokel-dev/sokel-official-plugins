package main

// kubeconfig → 配好认证的 http.Client + REST 调用。
//
// client-go 只用两个底层包：clientcmd（解析 kubeconfig：证书/token/exec 插件）与
// rest（TransportFor：拿到装好 mTLS/Bearer 的 RoundTripper）。资源读写本身是普通
// REST，路径自己拼——不引 typed clientset 全家桶（那是几百个生成类型，我们用 7 个路径）。

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
)

func credOf(ctx plugin.Ctx) Cred {
	var c Cred
	sokel.BindCredential(ctx, &c)
	return c
}

type kubeClient struct {
	base string // API server 地址（不带尾斜杠）
	hc   *http.Client
}

var (
	kcMu    sync.Mutex
	kcCache = map[string]*kubeClient{}
)

// clientOf 解析 kubeconfig 并构造 http.Client。按 kubeconfig 哈希缓存——
// TLS 握手与 exec 凭证插件都不便宜，同一凭证的连续节点复用连接。
func clientOf(cred Cred) (*kubeClient, error) {
	kc := strings.TrimSpace(cred.Kubeconfig)
	if kc == "" {
		return nil, fmt.Errorf("凭证缺 kubeconfig——ACK 用 aliyun 插件「导出 kubeconfig」取，自建集群粘 ~/.kube/config")
	}
	sum := sha256.Sum256([]byte(kc))
	key := hex.EncodeToString(sum[:8])
	kcMu.Lock()
	defer kcMu.Unlock()
	if c, ok := kcCache[key]; ok {
		return c, nil
	}
	cfg, err := clientcmd.RESTConfigFromKubeConfig([]byte(kc))
	if err != nil {
		return nil, fmt.Errorf("kubeconfig 解析失败: %w（要粘整份 YAML，不是只粘 server 地址）", err)
	}
	cfg.Timeout = 50 * time.Second
	rt, err := rest.TransportFor(cfg)
	if err != nil {
		return nil, fmt.Errorf("构造集群连接失败: %w", err)
	}
	c := &kubeClient{base: strings.TrimRight(cfg.Host, "/"), hc: &http.Client{Transport: rt, Timeout: 55 * time.Second}}
	kcCache[key] = c
	return c, nil
}

// do 一次 REST 调用。expectJSON=false 用于日志端点（返回纯文本）。
func (k *kubeClient) do(ctx context.Context, method, path string, query url.Values, body []byte, contentType string) ([]byte, error) {
	u := k.base + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	var rd io.Reader
	if body != nil {
		rd = strings.NewReader(string(body))
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := k.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("连接集群失败: %w（私网端点要求插件与集群网络可达）", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if resp.StatusCode >= 300 {
		return nil, kubeErr(resp.StatusCode, raw, path)
	}
	return raw, nil
}

// kubeErr K8s 的错误应答是标准 Status 对象，把常见几类翻译成下一步。
func kubeErr(code int, raw []byte, path string) error {
	var st struct {
		Message string `json:"message"`
		Reason  string `json:"reason"`
	}
	_ = json.Unmarshal(raw, &st)
	switch code {
	case http.StatusUnauthorized:
		return fmt.Errorf("集群拒绝了这份 kubeconfig（401）——证书/token 过期了，重新导出一份")
	case http.StatusForbidden:
		return fmt.Errorf("RBAC 不允许（403）：%s——给 kubeconfig 对应的主体加权限，或换权限够的 kubeconfig", st.Message)
	case http.StatusNotFound:
		return fmt.Errorf("找不到资源（%s）：%s——核对名字与命名空间", path, st.Message)
	}
	return fmt.Errorf("集群返回 HTTP %d：%s", code, firstNonEmpty(st.Message, string(raw[:min(len(raw), 200)])))
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// nsOf 操作级 > 凭证默认 > default。
func nsOf(cred Cred, override string) string {
	if n := strings.TrimSpace(override); n != "" {
		return n
	}
	if n := strings.TrimSpace(cred.Namespace); n != "" {
		return n
	}
	return "default"
}
