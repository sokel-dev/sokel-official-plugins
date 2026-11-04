package main

//go:generate go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen@v0.7.8 generate -lang go .
import (
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
	"log"
)

func clientFor(ctx plugin.Ctx) *client { return newClient(sokel.CredentialAs[Cred](ctx)) }
func register(p *sokel.Plugin) {
	p.SetVersion("0.1.1")
	RegisterCredential(p)
	RegisterDoc(p)
	registerOperations(p)
	DeclareEvents(p)
	sokel.RegisterWebhook(p, webhook)
}
func registerOperations(h plugin.Host) {
	OnHealthCheck(h, func(ctx plugin.Ctx, _ *HealthCheckIn) (*HealthCheckOut, error) { return clientFor(ctx).health(ctx) })
	OnListCustomers(h, func(ctx plugin.Ctx, in *ListCustomersIn) (*ListCustomersOut, error) {
		return clientFor(ctx).listCustomers(ctx, in)
	})
	OnGetCustomer(h, func(ctx plugin.Ctx, in *GetCustomerIn) (*GetCustomerOut, error) {
		return clientFor(ctx).getCustomer(ctx, in)
	})
	OnListSubscriptions(h, func(ctx plugin.Ctx, in *ListSubscriptionsIn) (*ListSubscriptionsOut, error) {
		return clientFor(ctx).listSubscriptions(ctx, in)
	})
	OnGetSubscription(h, func(ctx plugin.Ctx, in *GetSubscriptionIn) (*GetSubscriptionOut, error) {
		return clientFor(ctx).getSubscription(ctx, in)
	})
	OnListPaymentIntents(h, func(ctx plugin.Ctx, in *ListPaymentIntentsIn) (*ListPaymentIntentsOut, error) {
		return clientFor(ctx).listPaymentIntents(ctx, in)
	})
	OnGetPaymentIntent(h, func(ctx plugin.Ctx, in *GetPaymentIntentIn) (*GetPaymentIntentOut, error) {
		return clientFor(ctx).getPaymentIntent(ctx, in)
	})
}
func main() {
	p := sokel.New(sokel.Config{Endpoint: sokel.EnvOr("ENDPOINT", "http://localhost:8088"), Token: sokel.Env("TOKEN"), Name: "stripe"})
	register(p)
	if err := p.Run(); err != nil {
		log.Fatal(err)
	}
}
