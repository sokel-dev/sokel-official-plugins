// elasticsearch — a first-party Sokel plugin: Elasticsearch search and write.
//
// 18 operations cover three domains — search / document read-write / index management —
// plus a call fallback. Plain HTTP throughout; ES 7/8/9 and OpenSearch share the same
// code. See the top comment in schema/schema.go for the design rationale.
//
// Run: SOKEL_ENDPOINT=nats://<broker>:4222 SOKEL_TOKEN=skp_xxx ./elasticsearch
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
		Name:     "elasticsearch",
	})
	RegisterCredential(p)
	p.SetDoc(usageDoc, "")

	OnSearch(p, opSearch)
	OnCount(p, opCount)
	OnDocGet(p, opDocGet)
	OnDocIndex(p, opDocIndex)
	OnDocUpdate(p, opDocUpdate)
	OnDocDelete(p, opDocDelete)
	OnBulkIndex(p, opBulkIndex)
	OnDeleteByQuery(p, opDeleteByQuery)
	OnIndicesList(p, opIndicesList)
	OnIndexCreate(p, opIndexCreate)
	OnIndexDelete(p, opIndexDelete)
	OnMappingGet(p, opMappingGet)
	OnMappingPut(p, opMappingPut)
	OnAliasSwitch(p, opAliasSwitch)
	OnReindex(p, opReindex)
	OnClusterHealth(p, opClusterHealth)
	OnCall(p, opCall)
	OnHealthCheck(p, opHealthCheck)

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
