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
	b, _ := docs.ReadFile("docs/crawl4ai.md")
	p.SetDoc(string(b), "")
	registerOperations(p)
}
func registerOperations(h plugin.Host) {
	OnHealthCheck(h, func(ctx plugin.Ctx, in *HealthCheckIn) (*HealthCheckOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).health_check(ctx, in)
	})
	OnCrawl(h, func(ctx plugin.Ctx, in *CrawlIn) (*CrawlOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).crawl(ctx, in)
	})
	OnSubmitCrawl(h, func(ctx plugin.Ctx, in *SubmitCrawlIn) (*SubmitCrawlOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).submit_crawl(ctx, in)
	})
	OnGetTask(h, func(ctx plugin.Ctx, in *GetTaskIn) (*GetTaskOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).get_task(ctx, in)
	})
	OnMarkdown(h, func(ctx plugin.Ctx, in *MarkdownIn) (*MarkdownOut, error) {
		return newClient(sokel.CredentialAs[Cred](ctx)).markdown(ctx, in)
	})
}
func main() {
	p := sokel.New(sokel.Config{Name: "crawl4ai", Endpoint: sokel.EnvOr("ENDPOINT", "http://localhost:8088"), Token: sokel.Env("TOKEN")})
	register(p)
	if e := p.Run(); e != nil {
		log.Fatal(e)
	}
}
