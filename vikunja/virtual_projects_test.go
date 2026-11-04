package main

import (
	"context"
	"github.com/sokel-dev/sokel-official-plugins/vikunja/schema"
	"os"
	"testing"
)

func TestRecordedVirtualProjectPagination(t *testing.T) {
	b, e := os.ReadFile("testdata/recorded-project-page.json")
	if e != nil {
		t.Fatal(e)
	}
	c := fresh()
	withReply(c, 200, string(b))
	one := 1
	o, e := c.list_projects(context.Background(), &ListProjectsIn{Page: schema.Page{Size: &one}})
	if e != nil || o == nil {
		t.Fatal(o, e)
	}
	if len(o.Projects) != 2 || o.Projects[1].ID != "-2" || o.Total != "1" || o.HasMore {
		t.Fatal(o)
	}
}
