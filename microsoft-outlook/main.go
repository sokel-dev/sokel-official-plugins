package main

import (
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
	"log"
)

func main() {
	p := sokel.New(sokel.Config{Endpoint: sokel.EnvOr("ENDPOINT", "http://localhost:8088"), Token: sokel.Env("TOKEN"), Name: "microsoft-outlook"})
	p.SetVersion("0.1.0")
	p.SetDoc(usageDoc, "")
	RegisterCredential(p)
	registerOperations(p, func(c plugin.Ctx) *api { return newAPI(sokel.CredentialAs[Cred](c).AccessToken) })
	if e := p.Run(); e != nil {
		log.Fatal(e)
	}
}
func registerOperations(h plugin.Host, client func(plugin.Ctx) *api) {
	OnHealthCheck(h, func(c plugin.Ctx, in *HealthCheckIn) (*HealthCheckOut, error) { return client(c).health_check(c, in) })
	OnListMessages(h, func(c plugin.Ctx, in *ListMessagesIn) (*ListMessagesOut, error) {
		return client(c).list_messages(c, in)
	})
	OnGetMessage(h, func(c plugin.Ctx, in *GetMessageIn) (*GetMessageOut, error) { return client(c).get_message(c, in) })
	OnSendMail(h, func(c plugin.Ctx, in *SendMailIn) (*SendMailOut, error) { return client(c).send_mail(c, in) })
	OnListAttachments(h, func(c plugin.Ctx, in *ListAttachmentsIn) (*ListAttachmentsOut, error) {
		return client(c).list_attachments(c, in)
	})
	OnDownloadAttachment(h, func(c plugin.Ctx, in *DownloadAttachmentIn) (*DownloadAttachmentOut, error) {
		return client(c).download_attachment(c, in)
	})
	OnCalendarView(h, func(c plugin.Ctx, in *CalendarViewIn) (*CalendarViewOut, error) {
		return client(c).calendar_view(c, in)
	})
	OnCreateEvent(h, func(c plugin.Ctx, in *CreateEventIn) (*CreateEventOut, error) { return client(c).create_event(c, in) })
}
