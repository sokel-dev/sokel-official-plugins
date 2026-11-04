package main

import (
	"fmt"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
	"log"
)

func apiOf(ctx plugin.Ctx) *api { return newAPI(sokel.CredentialAs[Cred](ctx).AccessToken) }
func main() {
	p := sokel.New(sokel.Config{Endpoint: sokel.EnvOr("ENDPOINT", "http://localhost:8088"), Token: sokel.Env("TOKEN"), Name: "google-calendar"})
	p.SetVersion("0.1.0")
	p.SetDoc(usageDoc, "")
	RegisterCredential(p)
	RegisterAuth(p)
	DeclareEvents(p)
	registerOperations(p, apiOf)
	sokel.RegisterSource(p, sokel.Source{ID: "changes", Label: "Poll for changes every minute"}, runSource)
	if e := p.Run(); e != nil {
		log.Fatal(e)
	}
}

func registerOperations(h plugin.Host, getAPI func(plugin.Ctx) *api) {
	OnListEvents(h, func(ctx plugin.Ctx, in *ListEventsIn) (*ListEventsOut, error) { return getAPI(ctx).list(ctx, in) })
	OnGetEvent(h, func(ctx plugin.Ctx, in *GetEventIn) (*GetEventOut, error) {
		v, e := getAPI(ctx).get(ctx, in.CalendarID, in.EventID)
		return &GetEventOut{Event: v}, e
	})
	OnCreateEvent(h, func(ctx plugin.Ctx, in *CreateEventIn) (*CreateEventOut, error) {
		v, e := getAPI(ctx).save(ctx, "", in)
		return &CreateEventOut{Event: v}, e
	})
	OnUpdateEvent(h, func(ctx plugin.Ctx, in *UpdateEventIn) (*UpdateEventOut, error) {
		if in.EventID == "" {
			return nil, fmt.Errorf("event_id is required for update")
		}
		v, e := getAPI(ctx).save(ctx, in.EventID, &CreateEventIn{CalendarID: in.CalendarID, Summary: in.Summary, Description: in.Description, Location: in.Location, Start: in.Start, End: in.End, AllDay: in.AllDay, TimeZone: in.TimeZone, Attendees: in.Attendees, SendUpdates: in.SendUpdates})
		return &UpdateEventOut{Event: v}, e
	})
	OnDeleteEvent(h, func(ctx plugin.Ctx, in *DeleteEventIn) (*DeleteEventOut, error) {
		e := getAPI(ctx).remove(ctx, in)
		if e != nil {
			return nil, e
		}
		return &DeleteEventOut{OK: true}, nil
	})
	OnFreeBusy(h, func(ctx plugin.Ctx, in *FreeBusyIn) (*FreeBusyOut, error) { return getAPI(ctx).freebusy(ctx, in) })
	OnHealthCheck(h, func(ctx plugin.Ctx, _ *HealthCheckIn) (*HealthCheckOut, error) { return getAPI(ctx).health(ctx) })
}
