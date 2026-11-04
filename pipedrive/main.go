package main

//go:generate go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen@v0.7.8 generate .
import (
	"embed"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
	"log"
)

//go:embed docs/*.md
var docs embed.FS

func register(p *sokel.Plugin) {
	p.SetVersion("0.1.1")
	RegisterCredential(p)
	b, _ := docs.ReadFile("docs/pipedrive.md")
	p.SetDoc(string(b), "")
	registerOperations(p)
}
func registerOperations(h plugin.Host) {
	OnHealthCheck(h, func(ctx plugin.Ctx, in *HealthCheckIn) (*HealthCheckOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).health_check(ctx, in)
	})
	OnListPersons(h, func(ctx plugin.Ctx, in *ListPersonsIn) (*ListPersonsOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).list_persons(ctx, in)
	})
	OnGetPerson(h, func(ctx plugin.Ctx, in *GetPersonIn) (*GetPersonOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).get_person(ctx, in)
	})
	OnCreatePerson(h, func(ctx plugin.Ctx, in *CreatePersonIn) (*CreatePersonOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).create_person(ctx, in)
	})
	OnListOrganizations(h, func(ctx plugin.Ctx, in *ListOrganizationsIn) (*ListOrganizationsOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).list_organizations(ctx, in)
	})
	OnCreateOrganization(h, func(ctx plugin.Ctx, in *CreateOrganizationIn) (*CreateOrganizationOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).create_organization(ctx, in)
	})
	OnListDeals(h, func(ctx plugin.Ctx, in *ListDealsIn) (*ListDealsOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).list_deals(ctx, in)
	})
	OnCreateDeal(h, func(ctx plugin.Ctx, in *CreateDealIn) (*CreateDealOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).create_deal(ctx, in)
	})
	OnUpdateDeal(h, func(ctx plugin.Ctx, in *UpdateDealIn) (*UpdateDealOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).update_deal(ctx, in)
	})
}

func main() {
	p := sokel.New(sokel.Config{Name: "pipedrive", Endpoint: sokel.EnvOr("ENDPOINT", "http://localhost:8088"), Token: sokel.Env("TOKEN")})
	register(p)
	if e := p.Run(); e != nil {
		log.Fatal(e)
	}
}
