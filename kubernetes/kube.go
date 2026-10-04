package main

// kubeconfig -> an http.Client configured with auth + REST calls.
//
// Only two low-level client-go packages are used: clientcmd (parses kubeconfig:
// certs/token/exec plugins) and rest (TransportFor: gets a RoundTripper already set up with
// mTLS/Bearer). Resource reads/writes are plain REST with hand-built paths -- the typed
// clientset family isn't pulled in (that's hundreds of generated types; we use 7 paths).

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
	base string // API server address (no trailing slash)
	hc   *http.Client
}

var (
	kcMu    sync.Mutex
	kcCache = map[string]*kubeClient{}
)

// clientOf parses the kubeconfig and builds an http.Client. Cached by kubeconfig hash -- both
// the TLS handshake and exec credential plugins are expensive, so consecutive nodes on the same
// credential reuse the connection.
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

// do makes one REST call. expectJSON=false is used for log endpoints (which return plain text).
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

// kubeErr: K8s error responses are a standard Status object; translate the common cases into next steps.
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

// nsOf: operation-level override takes priority over the credential default, which takes priority over "default".
func nsOf(cred Cred, override string) string {
	if n := strings.TrimSpace(override); n != "" {
		return n
	}
	if n := strings.TrimSpace(cred.Namespace); n != "" {
		return n
	}
	return "default"
}
