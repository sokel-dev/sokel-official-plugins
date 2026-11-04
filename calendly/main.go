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
	p.SetVersion("0.1.1")
	RegisterCredential(p)
	b, _ := docs.ReadFile("docs/calendly.md")
	p.SetDoc(string(b), "")
	registerOperations(p)
}
func registerOperations(h plugin.Host) {
	OnHealthCheck(h, func(ctx plugin.Ctx, in *HealthCheckIn) (*HealthCheckOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).health_check(ctx, in)
	})
	OnGetCurrentUser(h, func(ctx plugin.Ctx, in *GetCurrentUserIn) (*GetCurrentUserOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).get_current_user(ctx, in)
	})
	OnListEventTypes(h, func(ctx plugin.Ctx, in *ListEventTypesIn) (*ListEventTypesOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).list_event_types(ctx, in)
	})
	OnGetEventType(h, func(ctx plugin.Ctx, in *GetEventTypeIn) (*GetEventTypeOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).get_event_type(ctx, in)
	})
	OnListEvents(h, func(ctx plugin.Ctx, in *ListEventsIn) (*ListEventsOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).list_events(ctx, in)
	})
	OnGetEvent(h, func(ctx plugin.Ctx, in *GetEventIn) (*GetEventOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).get_event(ctx, in)
	})
	OnListInvitees(h, func(ctx plugin.Ctx, in *ListInviteesIn) (*ListInviteesOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).list_invitees(ctx, in)
	})
	OnListAvailableTimes(h, func(ctx plugin.Ctx, in *ListAvailableTimesIn) (*ListAvailableTimesOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).list_available_times(ctx, in)
	})
	OnCreateSchedulingLink(h, func(ctx plugin.Ctx, in *CreateSchedulingLinkIn) (*CreateSchedulingLinkOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).create_scheduling_link(ctx, in)
	})
}
func main() {
	p := sokel.New(sokel.Config{Name: "calendly", Endpoint: sokel.EnvOr("ENDPOINT", "http://localhost:8088"), Token: sokel.Env("TOKEN")})
	register(p)
	if e := p.Run(); e != nil {
		log.Fatal(e)
	}
}
