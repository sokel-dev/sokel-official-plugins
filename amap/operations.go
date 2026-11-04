package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/sokel-dev/sokel-official-plugins/amap/schema"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

func (a *api) call(ctx context.Context, path string, q url.Values) (map[string]json.RawMessage, error) {
	raw, err := a.get(ctx, path, q)
	if err != nil {
		return nil, err
	}
	var env struct {
		Status string `json:"status"`
		Code   string `json:"infocode"`
	}
	if json.Unmarshal(raw, &env) != nil || env.Status == "" {
		return nil, fmt.Errorf("AMap response has no valid status")
	}
	if env.Status != "1" || (env.Code != "" && env.Code != "10000") {
		return nil, fmt.Errorf("AMap rejected the request; check key permissions, quota and parameters")
	}
	return object(raw)
}
func object(raw json.RawMessage) (map[string]json.RawMessage, error) {
	var m map[string]json.RawMessage
	if len(raw) == 0 || json.Unmarshal(raw, &m) != nil || m == nil {
		return nil, fmt.Errorf("AMap returned an invalid object")
	}
	return m, nil
}
func collection(raw json.RawMessage) ([]json.RawMessage, error) {
	var rows []json.RawMessage
	if len(raw) == 0 || json.Unmarshal(raw, &rows) != nil || rows == nil {
		return nil, fmt.Errorf("AMap response is missing a valid collection")
	}
	return rows, nil
}

// AMap documents [] for unavailable textual fields. Only an empty array is normalized.
func texts(raw json.RawMessage, keys ...string) (map[string]string, error) {
	m, err := object(raw)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, k := range keys {
		v := m[k]
		if len(v) == 0 || string(v) == "null" {
			continue
		}
		var s string
		if json.Unmarshal(v, &s) == nil {
			out[k] = s
			continue
		}
		var empty []json.RawMessage
		if json.Unmarshal(v, &empty) == nil && empty != nil && len(empty) == 0 {
			continue
		}
		return nil, fmt.Errorf("AMap returned an invalid text field")
	}
	return out, nil
}
func required(v, label string) error {
	if strings.TrimSpace(v) == "" || len(v) > 4096 || strings.ContainsAny(v, "\r\n\x00") {
		return fmt.Errorf("%s is required and must be at most 4096 bytes without control characters", label)
	}
	return nil
}

var coordinatePattern = regexp.MustCompile(`^-?[0-9]+(?:\.[0-9]{1,6})?,-?[0-9]+(?:\.[0-9]{1,6})?$`)

func coordinates(s string) error {
	if len(s) > 40 || !coordinatePattern.MatchString(s) {
		return fmt.Errorf("coordinates must be longitude,latitude with at most six decimal places")
	}
	parts := strings.Split(s, ",")
	lng, e1 := strconv.ParseFloat(parts[0], 64)
	lat, e2 := strconv.ParseFloat(parts[1], 64)
	if e1 != nil || e2 != nil || lng < -180 || lng > 180 || lat < -90 || lat > 90 {
		return fmt.Errorf("coordinates are outside longitude or latitude bounds")
	}
	return nil
}
func integer(p *int, def, min, max int, label string) (int, error) {
	n := def
	if p != nil {
		n = *p
	}
	if n < min || n > max {
		return 0, fmt.Errorf("%s must be between %d and %d", label, min, max)
	}
	return n, nil
}
func decodeGeocode(raw json.RawMessage) (schema.Geocode, error) {
	s, e := texts(raw, "formatted_address", "country", "province", "city", "district", "adcode", "location", "level")
	if e != nil {
		return schema.Geocode{}, e
	}
	if s["formatted_address"] == "" && s["location"] == "" {
		return schema.Geocode{}, fmt.Errorf("AMap geocode is missing address and coordinates")
	}
	return schema.Geocode{Address: s["formatted_address"], Country: s["country"], Province: s["province"], City: s["city"], District: s["district"], Adcode: s["adcode"], Location: s["location"], Level: s["level"]}, nil
}
func (a *api) GeocodeAddress(ctx context.Context, in *GeocodeAddressIn) (*GeocodeAddressOut, error) {
	if e := required(in.Address, "address"); e != nil {
		return nil, e
	}
	q := url.Values{"address": {in.Address}}
	if in.City != "" {
		q.Set("city", in.City)
	}
	env, e := a.call(ctx, "/v3/geocode/geo", q)
	if e != nil {
		return nil, e
	}
	rows, e := collection(env["geocodes"])
	if e != nil {
		return nil, e
	}
	out := &GeocodeAddressOut{Matches: []schema.Geocode{}}
	for _, row := range rows {
		v, e := decodeGeocode(row)
		if e != nil {
			return nil, e
		}
		out.Matches = append(out.Matches, v)
	}
	return out, nil
}
func (a *api) ReverseGeocode(ctx context.Context, in *ReverseGeocodeIn) (*ReverseGeocodeOut, error) {
	if e := coordinates(in.Location); e != nil {
		return nil, e
	}
	radius, e := integer(in.Options.Radius, 1000, 0, 3000, "radius")
	if e != nil {
		return nil, e
	}
	env, e := a.call(ctx, "/v3/geocode/regeo", url.Values{"location": {in.Location}, "radius": {strconv.Itoa(radius)}, "extensions": {"base"}})
	if e != nil {
		return nil, e
	}
	m, e := object(env["regeocode"])
	if e != nil {
		return nil, e
	}
	address, e := texts(env["regeocode"], "formatted_address")
	if e != nil {
		return nil, e
	}
	s, e := texts(m["addressComponent"], "country", "province", "city", "district", "adcode")
	if e != nil {
		return nil, e
	}
	if address["formatted_address"] == "" {
		return nil, fmt.Errorf("AMap reverse geocode is missing the formatted address")
	}
	return &ReverseGeocodeOut{Address: schema.Geocode{Address: address["formatted_address"], Location: in.Location, Country: s["country"], Province: s["province"], City: s["city"], District: s["district"], Adcode: s["adcode"]}}, nil
}
func searchQuery(o schema.SearchOptions) (url.Values, int, int, error) {
	page, e := integer(o.Page, 1, 1, 200, "page number")
	if e != nil {
		return nil, 0, 0, e
	}
	size, e := integer(o.PageSize, 10, 1, 25, "page size")
	if e != nil {
		return nil, 0, 0, e
	}
	if (page-1)*size >= 200 {
		return nil, 0, 0, fmt.Errorf("page starts beyond the 200-place recall limit; narrow the query")
	}
	q := url.Values{"page_num": {strconv.Itoa(page)}, "page_size": {strconv.Itoa(size)}}
	if o.Region != "" {
		q.Set("region", o.Region)
	}
	if o.CityLimit != nil {
		q.Set("city_limit", strconv.FormatBool(*o.CityLimit))
	}
	if o.Types != "" {
		q.Set("types", o.Types)
	}
	return q, page, size, nil
}
func (a *api) places(ctx context.Context, path string, q url.Values) ([]schema.Place, error) {
	env, e := a.call(ctx, path, q)
	if e != nil {
		return nil, e
	}
	rows, e := collection(env["pois"])
	if e != nil {
		return nil, e
	}
	out := []schema.Place{}
	for _, r := range rows {
		s, e := texts(r, "id", "name", "address", "location", "type", "typecode", "pname", "cityname", "adname", "adcode")
		if e != nil {
			return nil, e
		}
		if s["id"] == "" || s["name"] == "" {
			return nil, fmt.Errorf("AMap place is missing its ID or name")
		}
		out = append(out, schema.Place{ID: s["id"], Name: s["name"], Address: s["address"], Location: s["location"], Type: s["type"], Typecode: s["typecode"], Province: s["pname"], City: s["cityname"], District: s["adname"], Adcode: s["adcode"]})
	}
	return out, nil
}
func placePage(n, page, size int) (next int, more, limit bool) {
	if (page-1)*size+n >= 200 {
		return 0, false, true
	}
	if n >= size {
		return page + 1, true, false
	}
	return
}
func searchKeyword(v string) error {
	if !utf8.ValidString(v) || utf8.RuneCountInString(v) > 80 || strings.ContainsAny(v, "\r\n\x00") {
		return fmt.Errorf("search query must contain at most 80 valid characters without control characters")
	}
	return nil
}
func (a *api) SearchPlaces(ctx context.Context, in *SearchPlacesIn) (*SearchPlacesOut, error) {
	if e := required(in.Query, "query"); e != nil {
		return nil, e
	}
	if e := searchKeyword(in.Query); e != nil {
		return nil, e
	}
	q, p, s, e := searchQuery(in.Options)
	if e != nil {
		return nil, e
	}
	q.Set("keywords", in.Query)
	rows, e := a.places(ctx, "/v5/place/text", q)
	if e != nil {
		return nil, e
	}
	if len(rows) > s || (p-1)*s+len(rows) > 200 {
		return nil, fmt.Errorf("AMap returned places beyond the requested page or recall limit")
	}
	next, more, limit := placePage(len(rows), p, s)
	return &SearchPlacesOut{Places: rows, ReturnedCount: len(rows), NextPage: next, HasMore: more, PageLimitReached: limit}, nil
}
func (a *api) SearchNearby(ctx context.Context, in *SearchNearbyIn) (*SearchNearbyOut, error) {
	if e := coordinates(in.Location); e != nil {
		return nil, e
	}
	if e := searchKeyword(in.Query); e != nil {
		return nil, e
	}
	o := in.Options
	q, p, s, e := searchQuery(schema.SearchOptions{Region: o.Region, CityLimit: o.CityLimit, Types: o.Types, Page: o.Page, PageSize: o.PageSize})
	if e != nil {
		return nil, e
	}
	radius, e := integer(o.Radius, 5000, 0, 50000, "radius")
	if e != nil {
		return nil, e
	}
	sort := o.Sort
	if sort == "" {
		sort = "distance"
	}
	if sort != "distance" && sort != "weight" {
		return nil, fmt.Errorf("sort rule must be distance or weight")
	}
	q.Set("location", in.Location)
	q.Set("radius", strconv.Itoa(radius))
	q.Set("sortrule", sort)
	if in.Query != "" {
		q.Set("keywords", in.Query)
	}
	rows, e := a.places(ctx, "/v5/place/around", q)
	if e != nil {
		return nil, e
	}
	if len(rows) > s || (p-1)*s+len(rows) > 200 {
		return nil, fmt.Errorf("AMap returned places beyond the requested page or recall limit")
	}
	next, more, limit := placePage(len(rows), p, s)
	return &SearchNearbyOut{Places: rows, ReturnedCount: len(rows), NextPage: next, HasMore: more, PageLimitReached: limit}, nil
}
func (a *api) GetPlace(ctx context.Context, in *GetPlaceIn) (*GetPlaceOut, error) {
	if e := required(in.PlaceID, "place ID"); e != nil {
		return nil, e
	}
	rows, e := a.places(ctx, "/v5/place/detail", url.Values{"id": {in.PlaceID}})
	if e != nil {
		return nil, e
	}
	return &GetPlaceOut{Places: rows}, nil
}
func (a *api) PlanRoute(ctx context.Context, in *PlanRouteIn) (*PlanRouteOut, error) {
	if in.Mode != "driving" && in.Mode != "walking" {
		return nil, fmt.Errorf("travel mode must be driving or walking")
	}
	if e := coordinates(in.Origin); e != nil {
		return nil, e
	}
	if e := coordinates(in.Destination); e != nil {
		return nil, e
	}
	q := url.Values{"origin": {in.Origin}, "destination": {in.Destination}}
	if in.Mode == "driving" {
		s, e := integer(in.Options.Strategy, 0, 0, 20, "driving strategy")
		if e != nil {
			return nil, e
		}
		q.Set("strategy", strconv.Itoa(s))
		q.Set("extensions", "base")
	} else if in.Options.Strategy != nil {
		return nil, fmt.Errorf("driving strategy is only supported for driving")
	}
	env, e := a.call(ctx, "/v3/direction/"+in.Mode, q)
	if e != nil {
		return nil, e
	}
	route, e := object(env["route"])
	if e != nil {
		return nil, e
	}
	paths, e := collection(route["paths"])
	if e != nil {
		return nil, e
	}
	out := &PlanRouteOut{Routes: []schema.Route{}}
	for _, path := range paths {
		s, e := texts(path, "distance", "duration", "tolls")
		if e != nil {
			return nil, e
		}
		if s["distance"] == "" || s["duration"] == "" {
			return nil, fmt.Errorf("AMap route is missing distance or duration")
		}
		m, e := object(path)
		if e != nil {
			return nil, e
		}
		steps, e := collection(m["steps"])
		if e != nil {
			return nil, e
		}
		v := schema.Route{Distance: s["distance"], Duration: s["duration"], Tolls: s["tolls"], Steps: []schema.RouteStep{}}
		for _, step := range steps {
			s, e := texts(step, "instruction", "road", "distance", "duration", "polyline")
			if e != nil {
				return nil, e
			}
			v.Steps = append(v.Steps, schema.RouteStep{Instruction: s["instruction"], Road: s["road"], Distance: s["distance"], Duration: s["duration"], Polyline: s["polyline"]})
		}
		out.Routes = append(out.Routes, v)
	}
	return out, nil
}

var adcodePattern = regexp.MustCompile(`^[0-9]{6}$`)

func (a *api) GetWeather(ctx context.Context, in *GetWeatherIn) (*GetWeatherOut, error) {
	if !adcodePattern.MatchString(in.Adcode) {
		return nil, fmt.Errorf("administrative code must contain six digits")
	}
	env, e := a.call(ctx, "/v3/weather/weatherInfo", url.Values{"city": {in.Adcode}, "extensions": {"base"}})
	if e != nil {
		return nil, e
	}
	rows, e := collection(env["lives"])
	if e != nil {
		return nil, e
	}
	out := &GetWeatherOut{Weather: []schema.Weather{}}
	for _, r := range rows {
		s, e := texts(r, "province", "city", "adcode", "weather", "temperature", "humidity", "winddirection", "windpower", "reporttime")
		if e != nil {
			return nil, e
		}
		if s["adcode"] == "" {
			return nil, fmt.Errorf("AMap weather observation is missing its administrative code")
		}
		out.Weather = append(out.Weather, schema.Weather{Province: s["province"], City: s["city"], Adcode: s["adcode"], Weather: s["weather"], Temperature: s["temperature"], Humidity: s["humidity"], WindDirection: s["winddirection"], WindPower: s["windpower"], ReportTime: s["reporttime"]})
	}
	return out, nil
}
func flattenDistricts(rows []json.RawMessage, parent string, depth int, out *[]schema.District) error {
	if depth > 4 {
		return fmt.Errorf("AMap district tree exceeds requested depth")
	}
	for _, raw := range rows {
		if len(*out) >= 10000 {
			return fmt.Errorf("AMap district tree exceeds 10000 entries")
		}
		s, e := texts(raw, "name", "adcode", "citycode", "center", "level")
		if e != nil {
			return e
		}
		if s["name"] == "" || s["adcode"] == "" {
			return fmt.Errorf("AMap district is missing its name or administrative code")
		}
		*out = append(*out, schema.District{Name: s["name"], Adcode: s["adcode"], Citycode: s["citycode"], Center: s["center"], Level: s["level"], ParentAdcode: parent})
		m, e := object(raw)
		if e != nil {
			return e
		}
		if len(m["districts"]) > 0 && string(m["districts"]) != "null" {
			children, e := collection(m["districts"])
			if e != nil {
				return e
			}
			if len(children) > 0 {
				if e = flattenDistricts(children, s["adcode"], depth+1, out); e != nil {
					return e
				}
			}
		}
	}
	return nil
}
func (a *api) ListDistricts(ctx context.Context, in *ListDistrictsIn) (*ListDistrictsOut, error) {
	if e := required(in.Query, "district query"); e != nil {
		return nil, e
	}
	depth, e := integer(in.Options.Depth, 1, 0, 3, "child levels")
	if e != nil {
		return nil, e
	}
	page, e := integer(in.Options.Page, 1, 1, 10000, "page number")
	if e != nil {
		return nil, e
	}
	env, e := a.call(ctx, "/v3/config/district", url.Values{"keywords": {in.Query}, "subdistrict": {strconv.Itoa(depth)}, "page": {strconv.Itoa(page)}, "offset": {"20"}, "extensions": {"base"}})
	if e != nil {
		return nil, e
	}
	rows, e := collection(env["districts"])
	if e != nil {
		return nil, e
	}
	out := &ListDistrictsOut{Districts: []schema.District{}}
	if e = flattenDistricts(rows, "", 0, &out.Districts); e != nil {
		return nil, e
	}
	if len(rows) >= 20 {
		if page == 10000 {
			return nil, fmt.Errorf("district pagination exceeds the plugin limit of 10000 pages")
		}
		out.HasMore = true
		out.NextPage = page + 1
	}
	return out, nil
}
func (a *api) VerifyCredential(ctx context.Context, in *VerifyCredentialIn) (*VerifyCredentialOut, error) {
	if _, e := a.GeocodeAddress(ctx, &GeocodeAddressIn{Address: in.Address}); e != nil {
		return nil, e
	}
	return &VerifyCredentialOut{OK: true, Message: "AMap geocoding request succeeded; other services require their own permissions and quota"}, nil
}
