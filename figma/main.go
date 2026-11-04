package main

import (
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
	"log"
)

func main() {
	p := sokel.New(sokel.Config{Endpoint: sokel.EnvOr("ENDPOINT", "http://localhost:8088"), Token: sokel.Env("TOKEN"), Name: "figma"})
	p.SetVersion("0.1.0")
	p.SetDoc(usageDoc, "")
	RegisterCredential(p)
	registerOperations(p, func(ctx plugin.Ctx) *api { return newAPI(sokel.CredentialAs[Cred](ctx)) })
	if err := p.Run(); err != nil {
		log.Fatal(err)
	}
}
func registerOperations(h plugin.Host, c func(plugin.Ctx) *api) {
	OnHealthCheck(h, func(ctx plugin.Ctx, in *HealthCheckIn) (*HealthCheckOut, error) { return c(ctx).health(ctx, in) })
	OnGetFileMetadata(h, func(ctx plugin.Ctx, in *GetFileMetadataIn) (*GetFileMetadataOut, error) {
		return c(ctx).getFileMetadata(ctx, in)
	})
	OnGetNodes(h, func(ctx plugin.Ctx, in *GetNodesIn) (*GetNodesOut, error) { return c(ctx).getNodes(ctx, in) })
	OnRenderImages(h, func(ctx plugin.Ctx, in *RenderImagesIn) (*RenderImagesOut, error) {
		return c(ctx).renderImages(ctx, in)
	})
	OnListTeamFolders(h, func(ctx plugin.Ctx, in *ListTeamFoldersIn) (*ListTeamFoldersOut, error) {
		return c(ctx).listTeamFolders(ctx, in)
	})
	OnListSubfolders(h, func(ctx plugin.Ctx, in *ListSubfoldersIn) (*ListSubfoldersOut, error) {
		return c(ctx).listSubfolders(ctx, in)
	})
	OnListFolderFiles(h, func(ctx plugin.Ctx, in *ListFolderFilesIn) (*ListFolderFilesOut, error) {
		return c(ctx).listFolderFiles(ctx, in)
	})
	OnListComments(h, func(ctx plugin.Ctx, in *ListCommentsIn) (*ListCommentsOut, error) {
		return c(ctx).listComments(ctx, in)
	})
	OnCreateComment(h, func(ctx plugin.Ctx, in *CreateCommentIn) (*CreateCommentOut, error) {
		return c(ctx).createComment(ctx, in)
	})
}
