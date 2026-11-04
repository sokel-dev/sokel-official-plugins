package main

import (
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
	"log"
)

func main() {
	p := sokel.New(sokel.Config{Endpoint: sokel.EnvOr("ENDPOINT", "http://localhost:8088"), Token: sokel.Env("TOKEN"), Name: "salesforce"})
	p.SetVersion("0.1.0")
	p.SetDoc(usageDoc, "")
	RegisterCredential(p)
	registerOperations(p, func(c plugin.Ctx) *api { return newAPI(sokel.CredentialAs[Cred](c)) })
	if e := p.Run(); e != nil {
		log.Fatal(e)
	}
}
func registerOperations(h plugin.Host, client func(plugin.Ctx) *api) {
	OnHealthCheck(h, func(c plugin.Ctx, in *HealthCheckIn) (*HealthCheckOut, error) { return client(c).health_check(c, in) })
	OnDescribeObject(h, func(c plugin.Ctx, in *DescribeObjectIn) (*DescribeObjectOut, error) {
		return client(c).describe_object(c, in)
	})
	OnQuery(h, func(c plugin.Ctx, in *QueryIn) (*QueryOut, error) { return client(c).query(c, in) })
	OnQueryMore(h, func(c plugin.Ctx, in *QueryMoreIn) (*QueryMoreOut, error) { return client(c).query_more(c, in) })
	OnGetRecord(h, func(c plugin.Ctx, in *GetRecordIn) (*GetRecordOut, error) { return client(c).get_record(c, in) })
	OnCreateRecord(h, func(c plugin.Ctx, in *CreateRecordIn) (*CreateRecordOut, error) {
		return client(c).create_record(c, in)
	})
	OnUpdateRecord(h, func(c plugin.Ctx, in *UpdateRecordIn) (*UpdateRecordOut, error) {
		return client(c).update_record(c, in)
	})
	OnUpsertRecord(h, func(c plugin.Ctx, in *UpsertRecordIn) (*UpsertRecordOut, error) {
		return client(c).upsert_record(c, in)
	})
}
