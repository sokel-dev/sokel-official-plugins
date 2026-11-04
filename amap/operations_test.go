package main

import (
	"context"
	"encoding/json"
	"github.com/sokel-dev/sokel-official-plugins/amap/schema"
	"net/http"
	"strings"
	"testing"
)

// Synthetic fixtures based on official Web Service API shapes, not live account captures.
func TestGeocodingNormalizesDocumentedEmptyArrays(t *testing.T) {
	a := fakeAPI(t, func(r *http.Request) string {
		if r.URL.Host != "restapi.amap.com" || r.Method != "GET" || r.URL.Path != "/v3/geocode/geo" || r.URL.Query().Get("address") != "A+B 北京" || r.URL.Query().Get("city") != "北京" {
			t.Fatal(r.URL)
		}
		return `{"status":"1","infocode":"10000","geocodes":[{"formatted_address":"北京市","province":"北京市","city":[],"district":null,"adcode":"110000","location":"116.4,39.9","level":"城市"}]}`
	})
	o, e := a.GeocodeAddress(context.Background(), &GeocodeAddressIn{Address: "A+B 北京", City: "北京"})
	if e != nil {
		t.Fatal(e)
	}
	if len(o.Matches) != 1 || o.Matches[0].Address != "北京市" || o.Matches[0].City != "" {
		t.Fatal(o)
	}
}
func TestReverseGeocodeWire(t *testing.T) {
	a := fakeAPI(t, func(r *http.Request) string {
		q := r.URL.Query()
		if r.URL.Path != "/v3/geocode/regeo" || q.Get("radius") != "0" || q.Get("extensions") != "base" {
			t.Fatal(r.URL)
		}
		return `{"status":"1","infocode":"10000","regeocode":{"formatted_address":"北京市东城区","addressComponent":{"city":[],"country":"中国","province":"北京市","adcode":"110101"}}}`
	})
	o, e := a.ReverseGeocode(context.Background(), &ReverseGeocodeIn{Location: "116.4,39.9", Options: schema.ReverseOptions{Radius: ptr(0)}})
	if e != nil || o.Address.Adcode != "110101" || o.Address.Location != "116.4,39.9" {
		t.Fatal(o, e)
	}
}
func TestSearchV5PaginationAndFalse(t *testing.T) {
	a := fakeAPI(t, func(r *http.Request) string {
		q := r.URL.Query()
		if r.URL.Path != "/v5/place/text" || q.Get("page_size") != "1" || q.Get("page_num") != "2" || q.Get("city_limit") != "false" || q.Get("region") != "北京" {
			t.Fatal(r.URL)
		}
		return `{"status":"1","infocode":"10000","count":"999","pois":[{"id":"B123","name":"Place","location":"116.4,39.9","pname":"北京","cityname":"北京","adname":"东城","address":[]}]}`
	})
	o, e := a.SearchPlaces(context.Background(), &SearchPlacesIn{Query: "coffee", Options: schema.SearchOptions{Page: ptr(2), PageSize: ptr(1), Region: "北京", CityLimit: ptr(false)}})
	if e != nil {
		t.Fatal(e)
	}
	if !o.HasMore || o.NextPage != 3 || o.ReturnedCount != 1 || o.Places[0].Province != "北京" {
		t.Fatal(o)
	}
}
func TestNearbyAndPlacePaths(t *testing.T) {
	a := fakeAPI(t, func(r *http.Request) string {
		q := r.URL.Query()
		switch r.URL.Path {
		case "/v5/place/around":
			if q.Get("radius") != "0" || q.Get("sortrule") != "distance" || q.Get("location") != "0,0" {
				t.Fatal(q)
			}
		case "/v5/place/detail":
			if q.Get("id") != "B123" {
				t.Fatal(q)
			}
		default:
			t.Fatal(r.URL)
		}
		return `{"status":"1","infocode":"10000","pois":[]}`
	})
	c := context.Background()
	if _, e := a.SearchNearby(c, &SearchNearbyIn{Location: "0,0", Options: schema.NearbyOptions{Radius: ptr(0)}}); e != nil {
		t.Fatal(e)
	}
	if _, e := a.GetPlace(c, &GetPlaceIn{PlaceID: "B123"}); e != nil {
		t.Fatal(e)
	}
}
func TestRouteModesAndStepProjection(t *testing.T) {
	for _, mode := range []string{"driving", "walking"} {
		a := fakeAPI(t, func(r *http.Request) string {
			q := r.URL.Query()
			if r.URL.Path != "/v3/direction/"+mode || q.Get("origin") != "116.4,39.9" || q.Get("destination") != "116.5,40" {
				t.Fatal(r.URL)
			}
			if mode == "driving" && q.Get("strategy") != "0" {
				t.Fatal(q)
			}
			return `{"status":"1","infocode":"10000","route":{"paths":[{"distance":"10","duration":"3","tolls":[],"steps":[{"instruction":"Continue","road":[],"distance":"10","duration":"3","polyline":"116.4,39.9;116.5,40"}]}]}}`
		})
		in := PlanRouteIn{Mode: mode, Origin: "116.4,39.9", Destination: "116.5,40"}
		if mode == "driving" {
			in.Options.Strategy = ptr(0)
		}
		o, e := a.PlanRoute(context.Background(), &in)
		if e != nil || len(o.Routes) != 1 || o.Routes[0].Steps[0].Distance != "10" {
			t.Fatal(o, e)
		}
	}
}
func TestWeatherAndFlatDistricts(t *testing.T) {
	a := fakeAPI(t, func(r *http.Request) string {
		q := r.URL.Query()
		if r.URL.Path == "/v3/weather/weatherInfo" {
			if q.Get("city") != "110101" || q.Get("extensions") != "base" {
				t.Fatal(q)
			}
			return `{"status":"1","infocode":"10000","lives":[{"adcode":"110101","city":"东城区","temperature":"0","humidity":"50","reporttime":"2026-11-04 08:00:00"}]}`
		}
		if r.URL.Path != "/v3/config/district" || q.Get("subdistrict") != "1" {
			t.Fatal(r.URL)
		}
		return `{"status":"1","infocode":"10000","districts":[{"name":"北京","adcode":"110000","citycode":[],"level":"province","districts":[{"name":"东城","adcode":"110101","level":"district","districts":[]}]}]}`
	})
	c := context.Background()
	w, e := a.GetWeather(c, &GetWeatherIn{Adcode: "110101"})
	if e != nil || w.Weather[0].Temperature != "0" {
		t.Fatal(w, e)
	}
	d, e := a.ListDistricts(c, &ListDistrictsIn{Query: "北京"})
	if e != nil || len(d.Districts) != 2 || d.Districts[1].ParentAdcode != "110000" {
		t.Fatal(d, e)
	}
}
func TestExplicitQuotaProbe(t *testing.T) {
	calls := 0
	a := fakeAPI(t, func(r *http.Request) string { calls++; return `{"status":"1","infocode":"10000","geocodes":[]}` })
	c := context.Background()
	if _, e := a.VerifyCredential(c, &VerifyCredentialIn{}); e == nil || calls != 0 {
		t.Fatal(e, calls)
	}
	o, e := a.VerifyCredential(c, &VerifyCredentialIn{Address: "Beijing"})
	if e != nil || !o.OK || calls != 1 {
		t.Fatal(o, e, calls)
	}
}
func TestPageLimitDoesNotClaimExhaustion(t *testing.T) {
	pois := make([]map[string]string, 25)
	for i := range pois {
		pois[i] = map[string]string{"id": "B123", "name": "Place"}
	}
	raw, _ := json.Marshal(map[string]any{"status": "1", "infocode": "10000", "pois": pois})
	a := fakeAPI(t, func(*http.Request) string { return string(raw) })
	o, e := a.SearchPlaces(context.Background(), &SearchPlacesIn{Query: "q", Options: schema.SearchOptions{Page: ptr(8), PageSize: ptr(25)}})
	if e != nil || o.HasMore || !o.PageLimitReached || o.NextPage != 0 {
		t.Fatal(o, e)
	}
}
func TestMalformedAndBusinessFailures(t *testing.T) {
	for _, raw := range []string{`{}`, `{"status":"0","infocode":"10001","info":"test-secret"}`, `{"status":"1","infocode":"10000"}`, `{"status":"1","infocode":"10000","geocodes":[{"location":"116,39","city":["unexpected"]}]}`} {
		a := fakeAPI(t, func(*http.Request) string { return raw })
		if _, e := a.GeocodeAddress(context.Background(), &GeocodeAddressIn{Address: "q"}); e == nil || strings.Contains(e.Error(), "test-secret") {
			t.Fatal(raw, e)
		}
	}
}
func TestInvalidInputsDoNotSend(t *testing.T) {
	a := fakeAPI(t, func(*http.Request) string { t.Fatal("invalid request sent"); return "{}" })
	c := context.Background()
	for _, location := range []string{"181,0", "0,91", "NaN,0", "1.1234567,2", "1,2|3,4", "1e2,0"} {
		if _, e := a.ReverseGeocode(c, &ReverseGeocodeIn{Location: location}); e == nil {
			t.Fatal(location)
		}
	}
	if _, e := a.SearchPlaces(c, &SearchPlacesIn{Query: "q", Options: schema.SearchOptions{Page: ptr(0)}}); e == nil {
		t.Fatal("page0")
	}
	if _, e := a.PlanRoute(c, &PlanRouteIn{Mode: "walking", Origin: "0,0", Destination: "1,1", Options: schema.RouteOptions{Strategy: ptr(0)}}); e == nil {
		t.Fatal("driving options sent to walking")
	}
	if _, e := a.GetWeather(c, &GetWeatherIn{Adcode: "x"}); e == nil {
		t.Fatal("adcode")
	}
}
func TestSearchRecallCapAtTwoHundred(t *testing.T) {
	for _, tc := range []struct {
		page, size, n int
		limit, more   bool
		next          int
	}{{8, 25, 25, true, false, 0}, {67, 3, 2, true, false, 0}, {100, 1, 1, false, true, 101}, {200, 1, 1, true, false, 0}, {67, 3, 1, false, false, 0}} {
		pois := make([]map[string]string, tc.n)
		for i := range pois {
			pois[i] = map[string]string{"id": "B123", "name": "Place"}
		}
		raw, _ := json.Marshal(map[string]any{"status": "1", "pois": pois})
		a := fakeAPI(t, func(*http.Request) string { return string(raw) })
		o, e := a.SearchPlaces(context.Background(), &SearchPlacesIn{Query: "q", Options: schema.SearchOptions{Page: ptr(tc.page), PageSize: ptr(tc.size)}})
		if e != nil || o.PageLimitReached != tc.limit || o.HasMore != tc.more || o.NextPage != tc.next {
			t.Fatal(tc, o, e)
		}
	}
}
func TestOutOfRecallWindowAndLongSearchNeverSend(t *testing.T) {
	a := fakeAPI(t, func(*http.Request) string { t.Fatal("invalid request sent"); return "{}" })
	if _, e := a.SearchPlaces(context.Background(), &SearchPlacesIn{Query: "q", Options: schema.SearchOptions{Page: ptr(9), PageSize: ptr(25)}}); e == nil {
		t.Fatal("page exceeds 200 matches")
	}
	if _, e := a.SearchNearby(context.Background(), &SearchNearbyIn{Location: "0,0", Query: strings.Repeat("字", 81)}); e == nil {
		t.Fatal("search keyword exceeds 80 characters")
	}
}
