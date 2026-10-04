package main

// credFrom / withBlobMime：两个跨文件用到的小工具，单独放一处免得在 client.go 里显得突兀。

import (
	"context"
	"net/url"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
)

func credFrom(ctx plugin.Ctx) Cred { return sokel.CredentialAs[Cred](ctx) }

// withBlobMime：把 blob 的真实 Content-Type 带进 rpc（uploadBlob 不是 JSON 请求）。
func withBlobMime(ctx context.Context, mime string) context.Context {
	return context.WithValue(ctx, blobMimeKey{}, mime)
}

// queryOf：键值对 → url.Values（读接口的查询参数就那么几个，不值得每处写三行）。
func queryOf(kv ...string) url.Values {
	q := url.Values{}
	for i := 0; i+1 < len(kv); i += 2 {
		q.Set(kv[i], kv[i+1])
	}
	return q
}
