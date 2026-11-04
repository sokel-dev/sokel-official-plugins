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
	b, _ := docs.ReadFile("docs/trello.md")
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
	OnListLists(h, func(ctx plugin.Ctx, in *ListListsIn) (*ListListsOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).list_lists(ctx, in)
	})
	OnListCards(h, func(ctx plugin.Ctx, in *ListCardsIn) (*ListCardsOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).list_cards(ctx, in)
	})
	OnGetCard(h, func(ctx plugin.Ctx, in *GetCardIn) (*GetCardOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).get_card(ctx, in)
	})
	OnCreateCard(h, func(ctx plugin.Ctx, in *CreateCardIn) (*CreateCardOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).create_card(ctx, in)
	})
	OnUpdateCard(h, func(ctx plugin.Ctx, in *UpdateCardIn) (*UpdateCardOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).update_card(ctx, in)
	})
	OnAddComment(h, func(ctx plugin.Ctx, in *AddCommentIn) (*AddCommentOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).add_comment(ctx, in)
	})
}
func main() {
	p := sokel.New(sokel.Config{Name: "trello", Endpoint: sokel.EnvOr("ENDPOINT", "http://localhost:8088"), Token: sokel.Env("TOKEN")})
	register(p)
	if e := p.Run(); e != nil {
		log.Fatal(e)
	}
}
