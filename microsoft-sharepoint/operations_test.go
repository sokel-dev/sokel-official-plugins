package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

func TestGeneratedDiscoveryAndItemReads(t *testing.T) {
	c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1.0/sites":
			if r.URL.Query().Get("search") != "Research & 中文" {
				t.Fatal("search encoding")
			}
			reply(w, `{"value":[{"id":"tenant.sharepoint.com,site,web","name":"Research","displayName":"研究","webUrl":"https://tenant.sharepoint.com/sites/research"}]}`)
		case "/v1.0/sites/root":
			reply(w, `{"id":"tenant.sharepoint.com,site,web","name":"Research","webUrl":"https://tenant.sharepoint.com"}`)
		case "/v1.0/sites/site/drives":
			reply(w, `{"value":[{"id":"drive","name":"Documents","driveType":"documentLibrary"}]}`)
		case "/v1.0/sites/site/lists":
			reply(w, `{"value":[{"id":"list","name":"Issues","displayName":"Issues"}]}`)
		case "/v1.0/sites/site/lists/list/columns":
			reply(w, `{"value":[{"id":"col","name":"Title","displayName":"Title","required":true,"readOnly":false,"text":{}}]}`)
		case "/v1.0/sites/site/lists/list/items":
			if r.URL.Query().Get("$expand") != "fields" || r.URL.Query().Get("$top") != "100" {
				t.Fatal("missing expanded fields/page size")
			}
			reply(w, `{"value":[{"id":"1","eTag":"v1","fields":{"Title":"Issue","Count":0,"Done":false}}]}`)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	})
	invoke(t, c, "search_sites", `{"query":"Research & 中文"}`, nil)
	v := invoke(t, c, "get_site", `{"site_id":"root"}`, nil)
	if v["site"].(map[string]any)["id"] != "tenant.sharepoint.com,site,web" {
		t.Fatal(v)
	}
	invoke(t, c, "list_drives", `{"site_id":"site"}`, nil)
	invoke(t, c, "list_lists", `{"site_id":"site"}`, nil)
	v = invoke(t, c, "list_columns", `{"site_id":"site","list_id":"list"}`, nil)
	if v["columns"].([]any)[0].(map[string]any)["read_only"] != false {
		t.Fatal(v)
	}
	v = invoke(t, c, "list_items", `{"site_id":"site","list_id":"list"}`, nil)
	if v["items"].([]any)[0].(map[string]any)["fields"].(map[string]any)["Done"] != false {
		t.Fatal(v)
	}
}
func TestGeneratedCreateAndPatchScalarValues(t *testing.T) {
	c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if r.Header.Get("Authorization") != "Bearer test-access-token" {
			t.Fatal("missing auth")
		}
		switch r.Method {
		case "POST":
			if string(b) != `{"fields":{"Count":0,"Done":false,"Title":"你好"}}` {
				t.Fatalf("wrong creation bytes %s", b)
			}
			w.WriteHeader(201)
			reply(w, `{"id":"2","eTag":"v2","fields":{"Title":"你好","Count":0,"Done":false}}`)
		case "PATCH":
			if string(b) != `{"Done":false,"Title":null}` || r.Header.Get("If-Match") != "v2" {
				t.Fatalf("wrong patch bytes %s", b)
			}
			reply(w, `{"Title":null,"Done":false}`)
		default:
			t.Fatal("unexpected method")
		}
	})
	v := invoke(t, c, "create_item", `{"site_id":"site","list_id":"list","fields":{"Title":"你好","Count":0,"Done":false}}`, nil)
	if v["item"].(map[string]any)["id"] != "2" {
		t.Fatal(v)
	}
	v = invoke(t, c, "update_fields", `{"site_id":"site","list_id":"list","item_id":"2","if_match":"v2","fields":{"Title":null,"Done":false}}`, nil)
	if v["item_id"] != "2" {
		t.Fatal(v)
	}
}
func TestListPaginationAndValidation(t *testing.T) {
	next := graphOrigin + "/v1.0/sites/site/lists/list/items?$skiptoken=a%2Bb&$expand=fields"
	c := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("$skiptoken") == "" {
			reply(w, `{"value":[],"@odata.nextLink":"`+next+`"}`)
		} else {
			if r.URL.Query().Get("$skiptoken") != "a+b" {
				t.Fatal("cursor changed")
			}
			reply(w, `{"value":[]}`)
		}
	})
	out, e := c.listItems(context.Background(), &ListItemsIn{SiteID: "site", ListID: "list"})
	if e != nil || out.NextLink != next {
		t.Fatal(e)
	}
	out, e = c.listItems(context.Background(), &ListItemsIn{SiteID: "site", ListID: "list", NextLink: next})
	if e != nil || out.NextLink != "" {
		t.Fatal(e)
	}
	for _, fields := range []map[string]any{nil, {"@odata.bind": "bad"}, {"Title": []any{"a"}}, {"Title": map[string]any{"x": "y"}}, {"Count": json.Number("NaN")}} {
		if e := validateFields(fields); e == nil {
			t.Fatalf("invalid fields accepted %+v", fields)
		}
	}
	c = fixture(t, func(w http.ResponseWriter, r *http.Request) { reply(w, `{}`) })
	if _, e = c.updateFields(context.Background(), &UpdateFieldsIn{SiteID: "site", ListID: "list", ItemID: "1", Fields: map[string]any{"Title": "a"}}); e == nil {
		t.Fatal("empty patch receipt")
	}
	if _, e = c.createItem(context.Background(), &CreateItemIn{SiteID: "site", ListID: "list", Fields: map[string]any{"Title": "a"}}); e == nil {
		t.Fatal("wrong create status")
	}
	if _, e = c.listItems(context.Background(), &ListItemsIn{SiteID: "site", ListID: "list"}); e == nil {
		t.Fatal("missing collection")
	}
}
