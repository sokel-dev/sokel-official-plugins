package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/sokel-dev/sokel-official-plugins/qdrant/schema"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type captureEntry struct {
	Method   string          `json:"method"`
	Path     string          `json:"path"`
	Query    string          `json:"query"`
	Request  json.RawMessage `json:"request,omitempty"`
	Status   int             `json:"status"`
	Response json.RawMessage `json:"response"`
}

func localClient(t *testing.T) *client {
	t.Helper()
	target := os.Getenv("SOKEL_LIVE_QDRANT_URL")
	if target == "" {
		t.Skip("set the explicit local acceptance URL to test an isolated service")
	}
	u, e := url.Parse(target)
	if e != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
		t.Fatal("live tests require an isolated http://127.0.0.1:port URL")
	}
	c := newClient(Cred{APIKey: "wave6-local-test-only-key"})
	c.baseURL = target
	entries := []captureEntry{}
	c.http.Transport = transport(func(r *http.Request) (*http.Response, error) {
		request, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(request))
		res, e := http.DefaultTransport.RoundTrip(r)
		if e != nil {
			return nil, e
		}
		reply, e := io.ReadAll(io.LimitReader(res.Body, maxResponse+1))
		res.Body.Close()
		if e != nil {
			return nil, e
		}
		res.Body = io.NopCloser(bytes.NewReader(reply))
		entries = append(entries, captureEntry{r.Method, r.URL.Path, r.URL.RawQuery, json.RawMessage(request), res.StatusCode, json.RawMessage(reply)})
		return res, nil
	})
	t.Cleanup(func() {
		folder := os.Getenv("SOKEL_LIVE_FIXTURE_DIR")
		if folder == "" {
			return
		}
		abs, e := filepath.Abs(folder)
		if e != nil || (!strings.HasPrefix(abs, "/tmp/") && !strings.HasPrefix(abs, "/private/tmp/")) {
			t.Error("fixture directory must be under /tmp")
			return
		}
		if e = os.MkdirAll(abs, 0700); e != nil {
			t.Error(e)
			return
		}
		b, e := json.MarshalIndent(entries, "", "  ")
		if e != nil {
			t.Error(e)
			return
		}
		if e = os.WriteFile(filepath.Join(abs, "qdrant-operation-captures.json"), append(b, '\n'), 0600); e != nil {
			t.Error(e)
		}
	})
	return c
}
func TestLocalServiceAcceptance(t *testing.T) {
	c := localClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	name := fmt.Sprintf("sokel_wave6_%d", time.Now().UnixNano())
	h, e := c.health_check(ctx, &HealthCheckIn{})
	if e != nil || !h.OK {
		t.Fatal(h, e)
	}
	created, e := c.create_collection(ctx, &CreateCollectionIn{Collection: name, Size: 3, Distance: "Cosine"})
	if e != nil || !created.Created {
		t.Fatal(created, e)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, e := c.call(cleanup, "DELETE", "/collections/"+name, timeoutQuery(), nil); e != nil {
			t.Error(e)
		}
	})
	meta, e := c.get_collection(ctx, &GetCollectionIn{Collection: name})
	if e != nil || meta.Collection.Name != name {
		t.Fatal(meta, e)
	}
	payload := `{"tag":"book","count":9007199254740993,"available":false}`
	up, e := c.upsert_points(ctx, &UpsertPointsIn{Collection: name, Points: []schema.PointInput{{ID: "0", Vector: []float64{1, 0, 0}, PayloadJSON: &payload}, {ID: "18446744073709551615", Vector: []float64{0, 1, 0}, PayloadJSON: &payload}}})
	if e != nil || up.Receipt.Status != "completed" {
		t.Fatal(up, e)
	}
	got, e := c.retrieve_points(ctx, &RetrievePointsIn{Collection: name, IDs: []string{"18446744073709551615", "1"}})
	if e != nil || len(got.Points) != 1 || got.Points[0].ID != "18446744073709551615" || len(got.MissingIDs) != 1 || got.MissingIDs[0] != "1" || !strings.Contains(*got.Points[0].PayloadJSON, "9007199254740993") {
		t.Fatal(got, e)
	}
	page, e := c.scroll_points(ctx, &ScrollPointsIn{Collection: name, Options: schema.ScrollOptions{Limit: ptr(1)}})
	if e != nil || len(page.Points) != 1 || !page.HasMore {
		t.Fatal(page, e)
	}
	last, e := c.scroll_points(ctx, &ScrollPointsIn{Collection: name, Options: schema.ScrollOptions{Limit: ptr(1), Offset: &page.NextOffset}})
	if e != nil || len(last.Points) != 1 || last.Points[0].ID == page.Points[0].ID || last.HasMore {
		t.Fatal(last, e)
	}
	found, e := c.query_points(ctx, &QueryPointsIn{Collection: name, Vector: []float64{1, 0, 0}, Options: schema.QueryOptions{Limit: ptr(1), ScoreThreshold: ptr(0.0)}})
	if e != nil || len(found.Points) != 1 || found.Points[0].ID != "0" {
		t.Fatal(found, e)
	}
	deleted, e := c.delete_points(ctx, &DeletePointsIn{Collection: name, IDs: []string{"0"}})
	if e != nil || deleted.Receipt.Status != "completed" {
		t.Fatal(deleted, e)
	}
	gone, e := c.retrieve_points(ctx, &RetrievePointsIn{Collection: name, IDs: []string{"0"}})
	if e != nil || len(gone.Points) != 0 || len(gone.MissingIDs) != 1 {
		t.Fatal(gone, e)
	}
}
