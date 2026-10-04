// notion — a first-party Sokel plugin: read/write Notion plus data-source-change triggers.
//
// Three design decisions (see the top of schema/schema.go for details): model around
// **data sources** (since 2025-09-03, a database is just a container), page content goes through
// **markdown** (the block tree is only a fallback), and properties are given **both ways**
// (normalized props + raw properties_raw).
//
// Two auth methods coexist: an internal integration secret (paste the ntn_-prefixed token into the
// credential — simplest for self-hosting) or OAuth authorization (answered on the platform side by
// the notion provider, see server/internal/credential/oauth.go).
//
// Run: SOKEL_ENDPOINT=nats://<broker>:4222 SOKEL_TOKEN=skp_xxx ./notion
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
		Name:     "notion",
	})

	RegisterCredential(p)  // Credential contract (generated from the schema declaration; Cred type is in zz_credential.go)
	p.SetDoc(usageDoc, "") // Usage doc (docs/notion.md): how to set up the integration, share pages, wire up real-time events
	RegisterAuth(p)        // Auth method: Notion OAuth (no scopes — permissions are the pages the user checks on the consent screen)

	// Read
	OnNotionSearch(p, opSearch)
	OnNotionPageGet(p, opPageGet)
	OnNotionDBQuery(p, opDBQuery)
	OnNotionDBSchema(p, opDBSchema)
	OnNotionDBList(p, opDBList)
	OnNotionBlockChildren(p, opBlockChildren)
	OnNotionUserList(p, opUserList)
	OnNotionUserGet(p, opUserGet)
	OnNotionCommentList(p, opCommentList)
	OnHealthCheck(p, opHealthCheck) // Called by the "check" button on the credential page (id must be health_check)
	// Write
	OnNotionPageCreate(p, opPageCreate)
	OnNotionPageUpdate(p, opPageUpdate)
	OnNotionPageContent(p, opPageContent)
	OnNotionPageTrash(p, opPageTrash)
	OnNotionBlockAppend(p, opBlockAppend)
	OnNotionCommentCreate(p, opCommentCreate)
	OnNotionDBCreate(p, opDBCreate)
	OnNotionDBUpdateSchema(p, opDBUpdateSchema)
	OnNotionFileUpload(p, opFileUpload)

	DeclareEvents(p)
	// Long-running event source: polls the data sources configured on the credential (Notion's
	// webhooks can't be wired up for automation — see watch.go for why).
	sokel.RegisterSource(p, sokel.Source{ID: "watch", Label: "数据源变动轮询"}, runWatchSource)

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
