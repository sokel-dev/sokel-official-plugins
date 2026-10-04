package main

import (
	"net/http"
	"testing"
)

// The proxy can come from two sources, and **both must actually work**:
//
//	① the credential's "outbound proxy" — the production answer (a residential proxy to get around
//	   YouTube blocking datacenter IPs);
//	② the process's HTTP(S)_PROXY environment variables — the local-dev answer (direct connections from
//	   mainland China can't reach youtube.com at all).
//
// The first version only implemented ①: newClient built a bare &http.Transport{}, whose Proxy field is
// nil — which isn't "use the default", it's **explicitly disabling** the proxy (only
// http.DefaultTransport carries ProxyFromEnvironment). Symptom: exporting HTTPS_PROXY locally still
// couldn't reach YouTube, and it looked exactly like a network problem.
func TestClientProxySources(t *testing.T) {
	// ① when the credential provides one, use it, ignoring the environment.
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

	// ② when the credential gives nothing, the proxy **must not be disabled** — it should fall back to
	// the environment variables. This only asserts "not disabled", not a specific value:
	// ProxyFromEnvironment caches the environment it first reads via sync.Once internally, so changing
	// env vars in a test may not take effect, and asserting a specific value would make this a flaky
	// test.
	c2, err := newClient("", "")
	if err != nil {
		t.Fatal(err)
	}
	if c2.http.Transport.(*http.Transport).Proxy == nil {
		t.Error("没配凭证代理时应回落 ProxyFromEnvironment，而不是把代理整个关掉")
	}
}

func TestClientRejectsBadProxy(t *testing.T) {
	// The error must spell out the correct shape — just saying "invalid" leaves the user with no idea
	// what it should look like.
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
