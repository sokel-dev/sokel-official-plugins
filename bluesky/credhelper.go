package main

// credFrom / withBlobMime: two small helpers used across files, kept here on their own so they don't feel out of place in client.go.

import (
	"context"
	"net/url"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
)

func credFrom(ctx plugin.Ctx) Cred { return sokel.CredentialAs[Cred](ctx) }

// withBlobMime: carries the blob's real Content-Type into the rpc call (uploadBlob isn't a JSON request).
func withBlobMime(ctx context.Context, mime string) context.Context {
	return context.WithValue(ctx, blobMimeKey{}, mime)
}

// queryOf: key-value pairs → url.Values (read endpoints only ever need a few query params, not worth three lines each time).
func queryOf(kv ...string) url.Values {
	q := url.Values{}
	for i := 0; i+1 < len(kv); i += 2 {
		q.Set(kv[i], kv[i+1])
	}
	return q
}
