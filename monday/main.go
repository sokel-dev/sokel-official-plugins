package main

//go:generate go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen generate .
import (
	"embed"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
	"log"
)

//go:embed docs/*.md
var docs embed.FS

func register(p *sokel.Plugin) {
	p.SetVersion("0.1.0")
	RegisterCredential(p)
	b, _ := docs.ReadFile("docs/monday.md")
	p.SetDoc(string(b), "")
	registerOperations(p)
}
func registerOperations(h plugin.Host) {
	OnHealthCheck(h, func(ctx plugin.Ctx, in *HealthCheckIn) (*HealthCheckOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).health_check(ctx, in)
	})
	OnListBoards(h, func(ctx plugin.Ctx, in *ListBoardsIn) (*ListBoardsOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).list_boards(ctx, in)
	})
	OnGetBoard(h, func(ctx plugin.Ctx, in *GetBoardIn) (*GetBoardOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).get_board(ctx, in)
	})
	OnListItems(h, func(ctx plugin.Ctx, in *ListItemsIn) (*ListItemsOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).list_items(ctx, in)
	})
	OnGetItem(h, func(ctx plugin.Ctx, in *GetItemIn) (*GetItemOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).get_item(ctx, in)
	})
	OnCreateItem(h, func(ctx plugin.Ctx, in *CreateItemIn) (*CreateItemOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).create_item(ctx, in)
	})
	OnChangeColumns(h, func(ctx plugin.Ctx, in *ChangeColumnsIn) (*ChangeColumnsOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).change_columns(ctx, in)
	})
	OnAddUpdate(h, func(ctx plugin.Ctx, in *AddUpdateIn) (*AddUpdateOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).add_update(ctx, in)
	})
}

func main() {
	p := sokel.New(sokel.Config{Name: "monday", Endpoint: sokel.EnvOr("ENDPOINT", "http://localhost:8088"), Token: sokel.Env("TOKEN")})
	register(p)
	if e := p.Run(); e != nil {
		log.Fatal(e)
	}
}
