package main

import (
	"net/http"
	"testing"
)

// 代理有两个来源，**都得管用**：
//
//	① 凭证里的「出站代理」——线上正解（住宅代理对付 YouTube 封机房 IP）；
//	② 进程的 HTTP(S)_PROXY 环境变量——本地开发的正解（国内直连根本到不了 youtube.com）。
//
// 第一版只做了 ①：newClient 造了个 &http.Transport{}，而它的 Proxy 字段是 nil ——
// 那不是「用默认」，那是**显式关掉**代理（http.DefaultTransport 才带 ProxyFromEnvironment）。
// 症状：本地 export 了 HTTPS_PROXY 也照样连不上 YouTube，而且看起来像是网络问题。
func TestClientProxySources(t *testing.T) {
	// ① 凭证给了就用凭证的，不看环境。
	c, err := newClient("http://127.0.0.1:7897", "")
	if err != nil {
		t.Fatal(err)
	}
	tr := c.http.Transport.(*http.Transport)
	if tr.Proxy == nil {
		t.Fatal("配了凭证代理，Proxy 却是 nil")
	}
	req, _ := http.NewRequest(http.MethodGet, "https://www.youtube.com/", nil)
	u, err := tr.Proxy(req)
	if err != nil {
		t.Fatal(err)
	}
	if u == nil || u.String() != "http://127.0.0.1:7897" {
		t.Errorf("应走凭证里的代理，实际 %v", u)
	}

	// ② 凭证没给时**不能把代理关掉**——要回落到环境变量。
	// 这里只断言「没被关掉」，不断言具体取值：ProxyFromEnvironment 内部用 sync.Once
	// 缓存了首次读到的环境，测试里改 env 未必生效，断具体值会变成一条时灵时不灵的用例。
	c2, err := newClient("", "")
	if err != nil {
		t.Fatal(err)
	}
	if c2.http.Transport.(*http.Transport).Proxy == nil {
		t.Error("没配凭证代理时应回落 ProxyFromEnvironment，而不是把代理整个关掉")
	}
}

func TestClientRejectsBadProxy(t *testing.T) {
	// 报错要说清正确形状——只说「不合法」的话用户不知道该写成什么样。
	if _, err := newClient("://nonsense", ""); err == nil {
		t.Error("非法代理地址应报错")
	}
}

func TestClientDefaultUA(t *testing.T) {
	c, _ := newClient("", "   ")
	if c.ua != defaultUA {
		t.Errorf("UA 留空（或只有空格）应回落默认，实际 %q", c.ua)
	}
	c2, _ := newClient("", "MyBot/1.0")
	if c2.ua != "MyBot/1.0" {
		t.Errorf("填了就用填的，实际 %q", c2.ua)
	}
}
