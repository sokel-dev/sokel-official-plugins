package main

import "github.com/sokel-dev/sokel-plugin-sdk/plugin"

// Bot configuration operations: paving the way for webhook mode (setWebhook/deleteWebhook/
// getWebhookInfo) + the command menu (setMyCommands). All go through the generic callAPI
// (the token comes from the platform credential / env).

func opSetWebhook(ctx plugin.Ctx, in *SetWebhookIn) (*SetWebhookOut, error) {
	return okOut[SetWebhookOut](ctx, "setWebhook", compact(map[string]any{
		"url": in.URL, "secret_token": in.SecretToken,
		"allowed_updates": in.AllowedUpdates, "max_connections": in.MaxConnections,
	}))
}

func opDeleteWebhook(ctx plugin.Ctx, in *DeleteWebhookIn) (*DeleteWebhookOut, error) {
	return okOut[DeleteWebhookOut](ctx, "deleteWebhook", compact(map[string]any{"drop_pending_updates": in.DropPendingUpdates}))
}

func opGetWebhookInfo(ctx plugin.Ctx, in *GetWebhookInfoIn) (*GetWebhookInfoOut, error) {
	res, err := callAPI(ctx, "getWebhookInfo", nil)
	if err != nil {
		return nil, err
	}
	return remap[GetWebhookInfoOut](map[string]any{"ok": true, "result": res})
}

func opSetMyCommands(ctx plugin.Ctx, in *SetMyCommandsIn) (*SetMyCommandsOut, error) {
	return okOut[SetMyCommandsOut](ctx, "setMyCommands", map[string]any{"commands": in.Commands})
}
