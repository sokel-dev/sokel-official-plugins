package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestREDDataSourceRedaction(t *testing.T) {
	c := testClient(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "Bearer secret" || r.URL.Path != "/prefix/api/datasources" {
			t.Fatal("incorrect request")
		}
		return response("application/json", `[{"uid":"source","name":"Metrics","type":"prometheus","isDefault":false,"readOnly":false,"url":"http://secret","user":"secret","password":"secret","jsonData":{"secret":"secret"},"secureJsonFields":{"password":true}}]`), nil
	})
	o, e := c.listDataSources(ctxFor(""), &ListDataSourcesIn{})
	if e != nil {
		t.Fatal(e)
	}
	b, _ := json.Marshal(o)
	if strings.Contains(string(b), "secret") || len(o.DataSources) != 1 {
		t.Fatal(string(b))
	}
}
func TestREDCreateAnnotationPrecise(t *testing.T) {
	c := testClient(func(r *http.Request) (*http.Response, error) {
		b, _ := io.ReadAll(r.Body)
		if r.GetBody != nil || !strings.Contains(string(b), `"time":1507037197339`) {
			t.Fatalf("wire: %s", b)
		}
		return response("application/json", `{"id":9007199254740993,"message":"Annotation added"}`), nil
	})
	o, e := c.createAnnotation(ctxFor(""), &CreateAnnotationIn{Text: "Deploy", Time: "1507037197339"})
	if e != nil {
		t.Fatal(e)
	}
	if o.ID != "9007199254740993" {
		t.Fatal(o)
	}
}
