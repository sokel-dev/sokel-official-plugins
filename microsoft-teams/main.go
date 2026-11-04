package main

import (
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
	"log"
)

func main() {
	p := sokel.New(sokel.Config{Endpoint: sokel.EnvOr("ENDPOINT", "http://localhost:8088"), Token: sokel.Env("TOKEN"), Name: "microsoft-teams"})
	p.SetVersion("0.1.1")
	p.SetDoc(usageDoc, "")
	RegisterCredential(p)
	registerOperations(p, func(c plugin.Ctx) *api { return newAPI(sokel.CredentialAs[Cred](c).AccessToken) })
	if e := p.Run(); e != nil {
		log.Fatal(e)
	}
}
func registerOperations(h plugin.Host, client func(plugin.Ctx) *api) {
	OnHealthCheck(h, func(c plugin.Ctx, in *HealthCheckIn) (*HealthCheckOut, error) { return client(c).health_check(c, in) })
	OnListTeams(h, func(c plugin.Ctx, in *ListTeamsIn) (*ListTeamsOut, error) { return client(c).list_teams(c, in) })
	OnListChannels(h, func(c plugin.Ctx, in *ListChannelsIn) (*ListChannelsOut, error) {
		return client(c).list_channels(c, in)
	})
	OnListChats(h, func(c plugin.Ctx, in *ListChatsIn) (*ListChatsOut, error) { return client(c).list_chats(c, in) })
	OnListChannelMessages(h, func(c plugin.Ctx, in *ListChannelMessagesIn) (*ListChannelMessagesOut, error) {
		return client(c).list_channel_messages(c, in)
	})
	OnListReplies(h, func(c plugin.Ctx, in *ListRepliesIn) (*ListRepliesOut, error) { return client(c).list_replies(c, in) })
	OnListChatMessages(h, func(c plugin.Ctx, in *ListChatMessagesIn) (*ListChatMessagesOut, error) {
		return client(c).list_chat_messages(c, in)
	})
	OnSendChannelMessage(h, func(c plugin.Ctx, in *SendChannelMessageIn) (*SendChannelMessageOut, error) {
		return client(c).send_channel_message(c, in)
	})
	OnSendChatMessage(h, func(c plugin.Ctx, in *SendChatMessageIn) (*SendChatMessageOut, error) {
		return client(c).send_chat_message(c, in)
	})
}
