package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

type Shop struct {
	ID       string `json:"id" sokel:"id" label:"Shop ID"`
	Name     string `json:"name" sokel:"name" label:"Name"`
	Domain   string `json:"myshopifyDomain" sokel:"domain" label:"Shop domain"`
	Currency string `json:"currencyCode" sokel:"currency" label:"Currency"`
}
type Product struct {
	ID          string   `json:"id" sokel:"id" label:"Product ID"`
	Title       string   `json:"title" sokel:"title" label:"Title"`
	Handle      string   `json:"handle" sokel:"handle" label:"Handle"`
	Status      string   `json:"status" sokel:"status" label:"Status"`
	Vendor      string   `json:"vendor" sokel:"vendor" label:"Vendor"`
	ProductType string   `json:"productType" sokel:"product_type" label:"Product type"`
	Tags        []string `json:"tags" sokel:"tags" label:"Tags"`
	UpdatedAt   string   `json:"updatedAt" sokel:"updated_at" label:"Updated at"`
}
type Money struct {
	Amount   string `json:"amount" sokel:"amount" label:"Decimal amount"`
	Currency string `json:"currencyCode" sokel:"currency" label:"Currency"`
}
type Order struct {
	ID                string `json:"id" sokel:"id" label:"Order ID"`
	Name              string `json:"name" sokel:"name" label:"Order name"`
	CreatedAt         string `json:"created_at" sokel:"created_at" label:"Created at"`
	UpdatedAt         string `json:"updated_at" sokel:"updated_at" label:"Updated at"`
	FinancialStatus   string `json:"financial_status" sokel:"financial_status" label:"Financial status"`
	FulfillmentStatus string `json:"fulfillment_status" sokel:"fulfillment_status" label:"Fulfillment status"`
	Total             Money  `json:"total" sokel:"total" label:"Shop currency total"`
}
type InventoryItem struct {
	ID               string `json:"id" sokel:"id" label:"Inventory item ID"`
	SKU              string `json:"sku" sokel:"sku" label:"SKU"`
	Tracked          bool   `json:"tracked" sokel:"tracked" label:"Inventory tracked"`
	RequiresShipping bool   `json:"requiresShipping" sokel:"requires_shipping" label:"Requires shipping"`
	UpdatedAt        string `json:"updatedAt" sokel:"updated_at" label:"Updated at"`
}
type ProductChanges struct {
	Title  *string `json:"title,omitempty" sokel:"title,optional" label:"Title"`
	Status *string `json:"status,omitempty" sokel:"status,optional" label:"Status: ACTIVE, DRAFT or ARCHIVED"`
}
type HealthCheck struct{}

func (HealthCheck) Meta() contract.Meta {
	return contract.Meta{ID: "health_check", Label: "Check credential", TimeoutSec: 60}
}
func (HealthCheck) Inputs() []contract.FieldSpec { return []contract.FieldSpec{} }
func (HealthCheck) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("ok").Label("Available"), field.String("message").Label("Message")}
}

type GetShop struct{}

func (GetShop) Meta() contract.Meta {
	return contract.Meta{ID: "get_shop", Label: "Get shop", TimeoutSec: 60}
}
func (GetShop) Inputs() []contract.FieldSpec { return []contract.FieldSpec{} }
func (GetShop) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("shop", Shop{}).Label("Shop")}
}

type ListProducts struct{}

func (ListProducts) Meta() contract.Meta {
	return contract.Meta{ID: "list_products", Label: "List products", TimeoutSec: 60}
}
func (ListProducts) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Int("limit").Label("Page size").Optional(), field.String("cursor").Label("Pagination cursor").Optional(), field.String("query").Label("Shopify search query").Optional()}
}
func (ListProducts) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("products", []Product{}).Label("Products"), field.String("next_cursor").Label("Next cursor"), field.Bool("has_more").Label("More results")}
}

type GetProduct struct{}

func (GetProduct) Meta() contract.Meta {
	return contract.Meta{ID: "get_product", Label: "Get product", TimeoutSec: 60}
}
func (GetProduct) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("product_id").Label("Product GID")}
}
func (GetProduct) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("product", Product{}).Label("Product")}
}

type ListOrders struct{}

func (ListOrders) Meta() contract.Meta {
	return contract.Meta{ID: "list_orders", Label: "List orders", TimeoutSec: 60}
}
func (ListOrders) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Int("limit").Label("Page size").Optional(), field.String("cursor").Label("Pagination cursor").Optional(), field.String("query").Label("Shopify search query").Optional()}
}
func (ListOrders) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("orders", []Order{}).Label("Orders"), field.String("next_cursor").Label("Next cursor"), field.Bool("has_more").Label("More results")}
}

type GetOrder struct{}

func (GetOrder) Meta() contract.Meta {
	return contract.Meta{ID: "get_order", Label: "Get order", TimeoutSec: 60}
}
func (GetOrder) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("order_id").Label("Order GID")}
}
func (GetOrder) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("order", Order{}).Label("Order")}
}

type ListInventoryItems struct{}

func (ListInventoryItems) Meta() contract.Meta {
	return contract.Meta{ID: "list_inventory_items", Label: "List inventory items", TimeoutSec: 60}
}
func (ListInventoryItems) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Int("limit").Label("Page size").Optional(), field.String("cursor").Label("Pagination cursor").Optional(), field.String("query").Label("Shopify search query").Optional()}
}
func (ListInventoryItems) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("items", []InventoryItem{}).Label("Inventory items"), field.String("next_cursor").Label("Next cursor"), field.Bool("has_more").Label("More results")}
}

type UpdateProduct struct{}

func (UpdateProduct) Meta() contract.Meta {
	return contract.Meta{ID: "update_product", Label: "Update product", TimeoutSec: 60}
}
func (UpdateProduct) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("product_id").Label("Product GID"), field.Json("changes", ProductChanges{}).Label("Changed fields")}
}
func (UpdateProduct) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("product", Product{}).Label("Product")}
}

type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{field.Text("shop").Label("Shop subdomain").Desc("Use the shop name before .myshopify.com, without a URL"), field.Secret("access_token").Label("Admin API access token").Desc("Obtain an Admin API token for this shop with the required scopes; refresh externally")}
}
