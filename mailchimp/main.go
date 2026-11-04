package main

//go:generate go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen generate .
import (
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
	"log"
)

func clientFor(ctx plugin.Ctx) *client {
	cred := sokel.CredentialAs[Cred](ctx)
	return newClient(&cred)
}
func register(p *sokel.Plugin) {
	p.SetVersion("0.1.1")
	RegisterCredential(p)
	RegisterDoc(p)
	registerOperations(p)
}
func registerOperations(h plugin.Host) {
	OnHealthCheck(h, func(ctx plugin.Ctx, in *HealthCheckIn) (*HealthCheckOut, error) {
		return clientFor(ctx).health_check(ctx, in)
	})
	OnListAudiences(h, func(ctx plugin.Ctx, in *ListAudiencesIn) (*ListAudiencesOut, error) {
		return clientFor(ctx).list_audiences(ctx, in)
	})
	OnGetAudience(h, func(ctx plugin.Ctx, in *GetAudienceIn) (*GetAudienceOut, error) {
		return clientFor(ctx).get_audience(ctx, in)
	})
	OnListMembers(h, func(ctx plugin.Ctx, in *ListMembersIn) (*ListMembersOut, error) {
		return clientFor(ctx).list_members(ctx, in)
	})
	OnGetMember(h, func(ctx plugin.Ctx, in *GetMemberIn) (*GetMemberOut, error) {
		return clientFor(ctx).get_member(ctx, in)
	})
	OnUpsertMember(h, func(ctx plugin.Ctx, in *UpsertMemberIn) (*UpsertMemberOut, error) {
		return clientFor(ctx).upsert_member(ctx, in)
	})
	OnListMemberTags(h, func(ctx plugin.Ctx, in *ListMemberTagsIn) (*ListMemberTagsOut, error) {
		return clientFor(ctx).list_member_tags(ctx, in)
	})
	OnListCampaigns(h, func(ctx plugin.Ctx, in *ListCampaignsIn) (*ListCampaignsOut, error) {
		return clientFor(ctx).list_campaigns(ctx, in)
	})
}
func main() {
	p := sokel.New(sokel.Config{Endpoint: sokel.EnvOr("ENDPOINT", "http://localhost:8088"), Token: sokel.Env("TOKEN"), Name: "mailchimp"})
	register(p)
	if err := p.Run(); err != nil {
		log.Fatal(err)
	}
}
