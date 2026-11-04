package main

import (
	"context"
	"encoding/json"
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"io"
	"net/http"
	"testing"
)

type captureHost struct {
	ops    map[string]plugin.Invoke
	auth   contract.AuthMeta
	fields []contract.Field
}

func (h *captureHost) Register(op contract.Operation, fn plugin.Invoke)       { h.ops[op.ID] = fn }
func (h *captureHost) SetCredentialContract(f []contract.Field)               { h.fields = f }
func (h *captureHost) SetAuthFlow(m contract.AuthMeta, _ plugin.AuthHandlers) { h.auth = m }
func (h *captureHost) DeclareEvent(contract.Event)                            {}
func (h *captureHost) DeclareEventsCommon([]contract.Field, []string)         {}

type captureSink struct{ v any }

func (s *captureSink) Vars(v any) { s.v = v }
func (*captureSink) Text(string)  {}
func (*captureSink) JSON(any)     {}

type testCtx struct{ context.Context }

func (c *testCtx) Credential() map[string]string {
	return map[string]string{"access_token": "test-token", "api_key": "test-key"}
}
func (c *testCtx) Fetch(*plugin.File) ([]byte, error)                  { panic("unexpected file fetch") }
func (c *testCtx) Upload(string, string, []byte) (*plugin.File, error) { panic("unexpected upload") }
func (c *testCtx) UploadReader(string, string, io.Reader) (*plugin.File, error) {
	panic("unexpected upload")
}
func TestGeneratedDispatchAndNestedPlatformFields(t *testing.T) {
	a := fakeAPI(t, func(r *http.Request) string {
		var result string
		switch r.URL.Path {
		case "/v3/geocode/geo":
			result = `"geocodes":[{"formatted_address":"Beijing","location":"116,39"}]`
		case "/v3/geocode/regeo":
			if r.URL.Query().Get("radius") != "0" {
				t.Fatal(r.URL)
			}
			result = `"regeocode":{"formatted_address":"Beijing","addressComponent":{"adcode":"110000"}}`
		case "/v5/place/text", "/v5/place/around":
			if r.URL.Query().Get("city_limit") != "false" || r.URL.Query().Get("page_size") != "1" {
				t.Fatal(r.URL)
			}
			result = `"pois":[{"id":"B1","name":"Example","pname":"Province"}]`
		case "/v5/place/detail":
			result = `"pois":[]`
		case "/v3/direction/driving":
			if r.URL.Query().Get("strategy") != "0" {
				t.Fatal(r.URL)
			}
			result = `"route":{"paths":[{"distance":"100","duration":"10","steps":[{"distance":"50","duration":"5"}]}]}`
		case "/v3/weather/weatherInfo":
			result = `"lives":[{"adcode":"110000","temperature":"0","winddirection":"North"}]`
		case "/v3/config/district":
			if r.URL.Query().Get("subdistrict") != "0" {
				t.Fatal(r.URL)
			}
			result = `"districts":[{"name":"Beijing","adcode":"110000"}]`
		default:
			t.Fatal(r.URL)
		}
		return `{"status":"1","infocode":"10000",` + result + `}`
	})
	h := &captureHost{ops: map[string]plugin.Invoke{}}
	RegisterCredential(h)
	registerOperations(h, func(plugin.Ctx) *api { return a })
	if len(h.fields) != 2 || h.fields[0].Type != "secret" || h.fields[1].Type != "secret" {
		t.Fatal(h.fields)
	}
	inputs := map[string]string{"geocode_address": `{"address":"Beijing"}`, "reverse_geocode": `{"location":"116,39","options":{"radius":0}}`, "search_places": `{"query":"coffee","options":{"city_limit":false,"page_size":1}}`, "search_nearby": `{"location":"116,39","options":{"city_limit":false,"page_size":1,"radius":0}}`, "get_place": `{"place_id":"B1"}`, "plan_route": `{"mode":"driving","origin":"116,39","destination":"117,40","options":{"strategy":0}}`, "get_weather": `{"adcode":"110000"}`, "list_districts": `{"query":"Beijing","options":{"depth":0}}`, "verify_credential": `{"address":"Beijing"}`}
	if len(h.ops) != len(inputs) {
		t.Fatal(len(h.ops))
	}
	c := &testCtx{Context: context.Background()}
	for id, input := range inputs {
		t.Run(id, func(t *testing.T) {
			s := &captureSink{}
			if e := h.ops[id](c, json.RawMessage(input), s); e != nil || s.v == nil {
				t.Fatal(e, s.v)
			}
			vars := contract.StructToVars(s.v)
			raw, _ := json.Marshal(vars)
			var m map[string]any
			_ = json.Unmarshal(raw, &m)
			if len(m) == 0 {
				t.Fatal("empty platform output")
			}
			if id == "plan_route" {
				route := m["routes"].([]any)[0].(map[string]any)
				if route["distance_m"] != "100" || route["steps"].([]any)[0].(map[string]any)["duration_s"] != "5" {
					t.Fatal(m)
				}
			}
			if id == "get_weather" {
				w := m["weather"].([]any)[0].(map[string]any)
				if w["temperature_c"] != "0" || w["wind_direction"] != "North" {
					t.Fatal(m)
				}
			}
			if e := h.ops[id](c, json.RawMessage(`{bad`), s); e == nil {
				t.Fatal("malformed input accepted")
			}
		})
	}
}
