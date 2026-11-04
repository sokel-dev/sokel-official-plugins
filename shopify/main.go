package main

import (
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
	"log"
)

func main() {
	p := sokel.New(sokel.Config{Endpoint: sokel.EnvOr("ENDPOINT", "http://localhost:8088"), Token: sokel.Env("TOKEN"), Name: "shopify"})
	p.SetVersion("0.1.1")
	p.SetDoc(usageDoc, "")
	RegisterCredential(p)
	registerOperations(p, func(c plugin.Ctx) *api { return newAPI(sokel.CredentialAs[Cred](c)) })
	if e := p.Run(); e != nil {
		log.Fatal(e)
	}
}
func registerOperations(h plugin.Host, client func(plugin.Ctx) *api) {
	OnHealthCheck(h, func(c plugin.Ctx, in *HealthCheckIn) (*HealthCheckOut, error) { return client(c).health_check(c, in) })
	OnGetShop(h, func(c plugin.Ctx, in *GetShopIn) (*GetShopOut, error) { return client(c).get_shop(c, in) })
	OnListProducts(h, func(c plugin.Ctx, in *ListProductsIn) (*ListProductsOut, error) {
		return client(c).list_products(c, in)
	})
	OnGetProduct(h, func(c plugin.Ctx, in *GetProductIn) (*GetProductOut, error) { return client(c).get_product(c, in) })
	OnListOrders(h, func(c plugin.Ctx, in *ListOrdersIn) (*ListOrdersOut, error) { return client(c).list_orders(c, in) })
	OnGetOrder(h, func(c plugin.Ctx, in *GetOrderIn) (*GetOrderOut, error) { return client(c).get_order(c, in) })
	OnListInventoryItems(h, func(c plugin.Ctx, in *ListInventoryItemsIn) (*ListInventoryItemsOut, error) {
		return client(c).list_inventory_items(c, in)
	})
	OnUpdateProduct(h, func(c plugin.Ctx, in *UpdateProductIn) (*UpdateProductOut, error) {
		return client(c).update_product(c, in)
	})
}
