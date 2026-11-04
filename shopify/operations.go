package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/sokel-dev/sokel-official-plugins/shopify/schema"
	"regexp"
	"strings"
)

const productFields = "id title handle status vendor productType tags updatedAt"
const orderFields = "id name createdAt updatedAt displayFinancialStatus displayFulfillmentStatus currentTotalPriceSet { shopMoney { amount currencyCode } }"
const inventoryFields = "id sku tracked requiresShipping updatedAt"
const shopFields = "id name myshopifyDomain currencyCode"

var gidNumber = regexp.MustCompile(`^[1-9][0-9]*$`)

func validGID(id, kind string) error {
	prefix := "gid://shopify/" + kind + "/"
	if !strings.HasPrefix(id, prefix) || !gidNumber.MatchString(strings.TrimPrefix(id, prefix)) {
		return fmt.Errorf("provide a valid Shopify %s GID", kind)
	}
	return nil
}
func (a *api) graphql(c context.Context, query string, variables any) (map[string]json.RawMessage, error) {
	var r struct {
		Data   map[string]json.RawMessage `json:"data"`
		Errors []json.RawMessage          `json:"errors"`
	}
	status, e := a.request(c, "POST", "/admin/api/2026-10/graphql.json", nil, map[string]any{"query": query, "variables": variables}, &r)
	if e != nil {
		return nil, e
	}
	if status != 200 {
		return nil, fmt.Errorf("Shopify returned an unexpected GraphQL status")
	}
	if len(r.Errors) > 0 {
		return nil, fmt.Errorf("Shopify rejected the GraphQL request; check scopes, query filters and rate limits")
	}
	if r.Data == nil {
		return nil, fmt.Errorf("Shopify response is missing data")
	}
	return r.Data, nil
}
func node(raw json.RawMessage, out any) error {
	if len(raw) == 0 || string(raw) == "null" {
		return fmt.Errorf("Shopify resource was not found or is inaccessible")
	}
	return decodeJSON(raw, out)
}
func (a *api) getShop(c context.Context) (schema.Shop, error) {
	var v schema.Shop
	r, e := a.graphql(c, "query { shop { "+shopFields+" } }", nil)
	if e == nil {
		e = node(r["shop"], &v)
	}
	if e == nil {
		e = validGID(v.ID, "Shop")
	}
	return v, e
}
func (a *api) health_check(c context.Context, _ *HealthCheckIn) (*HealthCheckOut, error) {
	_, e := a.getShop(c)
	if e != nil {
		return &HealthCheckOut{Message: e.Error()}, nil
	}
	return &HealthCheckOut{OK: true, Message: "Shopify credential is valid; operation scopes are checked separately"}, nil
}
func (a *api) get_shop(c context.Context, _ *GetShopIn) (*GetShopOut, error) {
	v, e := a.getShop(c)
	if e != nil {
		return nil, e
	}
	return &GetShopOut{Shop: v}, nil
}
func (a *api) connection(c context.Context, key, fields string, limit int, cursor, query string, out any) (string, bool, error) {
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 100 || len(cursor) > 8192 || len(query) > 8192 {
		return "", false, fmt.Errorf("page size must be 1 to 100; cursor and query must not exceed 8192 characters")
	}
	vars := map[string]any{"first": limit}
	if cursor != "" {
		vars["after"] = cursor
	}
	if query != "" {
		vars["query"] = query
	}
	r, e := a.graphql(c, "query($first:Int!,$after:String,$query:String){ "+key+"(first:$first,after:$after,query:$query){ nodes { "+fields+" } pageInfo { hasNextPage endCursor } } }", vars)
	if e != nil {
		return "", false, e
	}
	var v struct {
		Nodes    json.RawMessage `json:"nodes"`
		PageInfo *struct {
			HasNextPage *bool  `json:"hasNextPage"`
			EndCursor   string `json:"endCursor"`
		} `json:"pageInfo"`
	}
	if e = node(r[key], &v); e != nil {
		return "", false, e
	}
	if v.PageInfo == nil || v.PageInfo.HasNextPage == nil || len(v.Nodes) == 0 || v.Nodes[0] != '[' {
		return "", false, fmt.Errorf("Shopify response is missing connection fields")
	}
	if e = decodeJSON(v.Nodes, out); e != nil {
		return "", false, e
	}
	more := *v.PageInfo.HasNextPage
	if more && (v.PageInfo.EndCursor == "" || v.PageInfo.EndCursor == cursor) {
		return "", false, fmt.Errorf("Shopify pagination cursor did not advance")
	}
	if !more {
		return "", false, nil
	}
	return v.PageInfo.EndCursor, true, nil
}
func (a *api) list_products(c context.Context, in *ListProductsIn) (*ListProductsOut, error) {
	var v []schema.Product
	next, more, e := a.connection(c, "products", productFields, in.Limit, in.Cursor, in.Query, &v)
	if e != nil {
		return nil, e
	}
	for _, p := range v {
		if e = validGID(p.ID, "Product"); e != nil {
			return nil, e
		}
	}
	return &ListProductsOut{Products: v, NextCursor: next, HasMore: more}, nil
}
func (a *api) get_product(c context.Context, in *GetProductIn) (*GetProductOut, error) {
	if e := validGID(in.ProductID, "Product"); e != nil {
		return nil, e
	}
	r, e := a.graphql(c, "query($id:ID!){ product(id:$id){ "+productFields+" } }", map[string]any{"id": in.ProductID})
	if e != nil {
		return nil, e
	}
	var v schema.Product
	if e = node(r["product"], &v); e != nil {
		return nil, e
	}
	if v.ID != in.ProductID {
		return nil, fmt.Errorf("Shopify returned an unexpected product ID")
	}
	return &GetProductOut{Product: v}, nil
}

type wireOrder struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	CreatedAt         string `json:"createdAt"`
	UpdatedAt         string `json:"updatedAt"`
	FinancialStatus   string `json:"displayFinancialStatus"`
	FulfillmentStatus string `json:"displayFulfillmentStatus"`
	Total             *struct {
		Money *schema.Money `json:"shopMoney"`
	} `json:"currentTotalPriceSet"`
}

func (o wireOrder) output() (schema.Order, error) {
	if e := validGID(o.ID, "Order"); e != nil {
		return schema.Order{}, e
	}
	if o.Total == nil || o.Total.Money == nil || o.Total.Money.Amount == "" || o.Total.Money.Currency == "" {
		return schema.Order{}, fmt.Errorf("Shopify order is missing its total amount")
	}
	return schema.Order{ID: o.ID, Name: o.Name, CreatedAt: o.CreatedAt, UpdatedAt: o.UpdatedAt, FinancialStatus: o.FinancialStatus, FulfillmentStatus: o.FulfillmentStatus, Total: *o.Total.Money}, nil
}
func (a *api) list_orders(c context.Context, in *ListOrdersIn) (*ListOrdersOut, error) {
	var raw []wireOrder
	next, more, e := a.connection(c, "orders", orderFields, in.Limit, in.Cursor, in.Query, &raw)
	if e != nil {
		return nil, e
	}
	v := make([]schema.Order, 0, len(raw))
	for _, o := range raw {
		item, e := o.output()
		if e != nil {
			return nil, e
		}
		v = append(v, item)
	}
	return &ListOrdersOut{Orders: v, NextCursor: next, HasMore: more}, nil
}
func (a *api) get_order(c context.Context, in *GetOrderIn) (*GetOrderOut, error) {
	if e := validGID(in.OrderID, "Order"); e != nil {
		return nil, e
	}
	r, e := a.graphql(c, "query($id:ID!){ order(id:$id){ "+orderFields+" } }", map[string]any{"id": in.OrderID})
	if e != nil {
		return nil, e
	}
	var raw wireOrder
	if e = node(r["order"], &raw); e != nil {
		return nil, e
	}
	v, e := raw.output()
	if e != nil {
		return nil, e
	}
	if v.ID != in.OrderID {
		return nil, fmt.Errorf("Shopify returned an unexpected order ID")
	}
	return &GetOrderOut{Order: v}, nil
}
func (a *api) list_inventory_items(c context.Context, in *ListInventoryItemsIn) (*ListInventoryItemsOut, error) {
	var v []schema.InventoryItem
	next, more, e := a.connection(c, "inventoryItems", inventoryFields, in.Limit, in.Cursor, in.Query, &v)
	if e != nil {
		return nil, e
	}
	for _, p := range v {
		if e = validGID(p.ID, "InventoryItem"); e != nil {
			return nil, e
		}
	}
	return &ListInventoryItemsOut{Items: v, NextCursor: next, HasMore: more}, nil
}
func (a *api) update_product(c context.Context, in *UpdateProductIn) (*UpdateProductOut, error) {
	if e := validGID(in.ProductID, "Product"); e != nil {
		return nil, e
	}
	p := map[string]any{"id": in.ProductID}
	if in.Changes.Title != nil {
		if strings.TrimSpace(*in.Changes.Title) == "" {
			return nil, fmt.Errorf("product title must not be empty")
		}
		p["title"] = *in.Changes.Title
	}
	if in.Changes.Status != nil {
		v := *in.Changes.Status
		if v != "ACTIVE" && v != "DRAFT" && v != "ARCHIVED" {
			return nil, fmt.Errorf("product status must be ACTIVE, DRAFT or ARCHIVED")
		}
		p["status"] = v
	}
	if len(p) == 1 {
		return nil, fmt.Errorf("at least one changed field is required")
	}
	r, e := a.graphql(c, "mutation($product:ProductUpdateInput!){ productUpdate(product:$product){ product { "+productFields+" } userErrors { field message } } }", map[string]any{"product": p})
	if e != nil {
		return nil, e
	}
	var v struct {
		Product    *schema.Product   `json:"product"`
		UserErrors []json.RawMessage `json:"userErrors"`
	}
	if e = node(r["productUpdate"], &v); e != nil {
		return nil, e
	}
	if v.UserErrors == nil || len(v.UserErrors) > 0 || v.Product == nil || v.Product.ID != in.ProductID {
		return nil, fmt.Errorf("Shopify did not confirm the product update; check permissions and field values")
	}
	return &UpdateProductOut{Product: *v.Product}, nil
}
