package main

import (
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
	"log"
)

func main() {
	p := sokel.New(sokel.Config{Endpoint: sokel.EnvOr("ENDPOINT", "http://localhost:8088"), Token: sokel.Env("TOKEN"), Name: "zoom"})
	p.SetVersion("0.1.1")
	p.SetDoc(usageDoc, "")
	RegisterCredential(p)
	registerOperations(p, func(ctx plugin.Ctx) *api { return newAPI(sokel.CredentialAs[Cred](ctx)) })
	if err := p.Run(); err != nil {
		log.Fatal(err)
	}
}
func registerOperations(h plugin.Host, c func(plugin.Ctx) *api) {
	OnHealthCheck(h, func(ctx plugin.Ctx, in *HealthCheckIn) (*HealthCheckOut, error) { return c(ctx).health(ctx, in) })
	OnGetUser(h, func(ctx plugin.Ctx, in *GetUserIn) (*GetUserOut, error) { return c(ctx).getUser(ctx, in) })
	OnListUsers(h, func(ctx plugin.Ctx, in *ListUsersIn) (*ListUsersOut, error) { return c(ctx).listUsers(ctx, in) })
	OnListMeetings(h, func(ctx plugin.Ctx, in *ListMeetingsIn) (*ListMeetingsOut, error) {
		return c(ctx).listMeetings(ctx, in)
	})
	OnGetMeeting(h, func(ctx plugin.Ctx, in *GetMeetingIn) (*GetMeetingOut, error) { return c(ctx).getMeeting(ctx, in) })
	OnCreateMeeting(h, func(ctx plugin.Ctx, in *CreateMeetingIn) (*CreateMeetingOut, error) {
		return c(ctx).createMeeting(ctx, in)
	})
	OnUpdateMeeting(h, func(ctx plugin.Ctx, in *UpdateMeetingIn) (*UpdateMeetingOut, error) {
		return c(ctx).updateMeeting(ctx, in)
	})
}
