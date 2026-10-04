// x — a first-party Sokel plugin: read/write and event triggers for X (Twitter).
//
// Five design decisions (see the top of schema/schema.go for details): reads use the platform's
// **incremental cursor** shape instead of copying X's next_token, **retweets/replies are
// distinguished at a glance** (kind is derived by the plugin), **a thread is one operation**,
// **media upload is its own operation**, and **write operations always return an id and a link**.
//
// There's only one auth path: platform-side OAuth 2.0 authorization (provider=x; PKCE and refresh
// rotation both live on the platform, see server/internal/credential/oauth.go). X has charged
// per-call since 2026-02, and each operation's cost is documented in the usage doc.
//
// Run: SOKEL_ENDPOINT=nats://<broker>:4222 SOKEL_TOKEN=skp_xxx ./x
package main

//go:generate go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen

import (
	"log"
	"os"

	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
)

func main() {
	token := sokel.Env("TOKEN")
	if token == "" && sokel.Env("DEPLOY_KEY") == "" {
		log.Fatal("请设置 SOKEL_TOKEN(接入组「接入命令」里复制);随部署托管的容器可改设 SOKEL_DEPLOY_KEY 自动注册")
	}
	p := sokel.New(sokel.Config{
		Endpoint: env("SOKEL_ENDPOINT", "http://localhost:8088"),
		Token:    token,
		Name:     "x",
	})

	RegisterCredential(p)  // Credential contract (generated from the schema declaration; Cred type is in zz_credential.go)
	p.SetDoc(usageDoc, "") // Usage doc (docs/x.md): how to set up the app, which scopes to check, what it costs
	RegisterAuth(p)        // Auth method: X's OAuth 2.0 (scopes are declared in the schema and must all be granted at once)

	// Publishing
	OnXPostCreate(p, opPostCreate)
	OnXPostThread(p, opPostThread)
	OnXPostDelete(p, opPostDelete)
	// Media
	OnXMediaUpload(p, opMediaUpload)
	// Engagement
	OnXLike(p, opLike)
	OnXUnlike(p, opUnlike)
	OnXRepost(p, opRepost)
	OnXUnrepost(p, opUnrepost)
	OnXBookmark(p, opBookmark)
	OnXUnbookmark(p, opUnbookmark)
	OnXFollow(p, opFollow)
	OnXUnfollow(p, opUnfollow)
	// Reading
	OnXSearch(p, opSearch)
	OnXUserTimeline(p, opUserTimeline)
	OnXMentions(p, opMentions)
	OnXListPosts(p, opListPosts)
	OnXPostGet(p, opPostGet)
	OnXUserGet(p, opUserGet)
	// Health check (the operation id the platform expects: both the credential page's "test" and
	// the workflow's "check credential" call it)
	OnHealthCheck(p, opHealthCheck)
	// Direct messages
	OnXDmSend(p, opDMSend)
	OnXDmEvents(p, opDMEvents)
	// Lists
	OnXListMemberAdd(p, opListMemberAdd)
	OnXListMemberRemove(p, opListMemberRemove)

	DeclareEvents(p)
	// Long-running event source: polls mentions and keywords (X's real-time push is only
	// available on the Enterprise tier, see watch.go).
	sokel.RegisterSource(p, sokel.Source{ID: "watch", Label: "提及/关键词轮询"}, runWatchSource)

	if err := p.Run(); err != nil {
		log.Fatal(err)
	}
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
