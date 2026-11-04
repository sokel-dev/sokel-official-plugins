package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

type Geocode struct {
	Address  string `json:"address" sokel:"address" label:"Formatted address"`
	Country  string `json:"country" sokel:"country" label:"Country"`
	Province string `json:"province" sokel:"province" label:"Province"`
	City     string `json:"city" sokel:"city" label:"City"`
	District string `json:"district" sokel:"district" label:"District"`
	Adcode   string `json:"adcode" sokel:"adcode" label:"Administrative code"`
	Location string `json:"location" sokel:"location" label:"Longitude and latitude"`
	Level    string `json:"level" sokel:"level" label:"Match level"`
}
type Place struct {
	ID       string `json:"id" sokel:"id" label:"Place ID"`
	Name     string `json:"name" sokel:"name" label:"Name"`
	Address  string `json:"address" sokel:"address" label:"Address"`
	Location string `json:"location" sokel:"location" label:"Longitude and latitude"`
	Type     string `json:"type" sokel:"type" label:"Place category"`
	Typecode string `json:"typecode" sokel:"typecode" label:"Category code"`
	Province string `json:"province" sokel:"province" label:"Province"`
	City     string `json:"city" sokel:"city" label:"City"`
	District string `json:"district" sokel:"district" label:"District"`
	Adcode   string `json:"adcode" sokel:"adcode" label:"Administrative code"`
}
type SearchOptions struct {
	Region    string `json:"region,omitempty" sokel:"region,optional" label:"Region"`
	CityLimit *bool  `json:"city_limit,omitempty" sokel:"city_limit,optional" label:"Limit to region"`
	Types     string `json:"types,omitempty" sokel:"types,optional" label:"Category codes"`
	Page      *int   `json:"page,omitempty" sokel:"page,optional" label:"Page number"`
	PageSize  *int   `json:"page_size,omitempty" sokel:"page_size,optional" label:"Page size"`
}
type NearbyOptions struct {
	Region    string `json:"region,omitempty" sokel:"region,optional" label:"Region"`
	CityLimit *bool  `json:"city_limit,omitempty" sokel:"city_limit,optional" label:"Limit to region"`
	Types     string `json:"types,omitempty" sokel:"types,optional" label:"Category codes"`
	Page      *int   `json:"page,omitempty" sokel:"page,optional" label:"Page number"`
	PageSize  *int   `json:"page_size,omitempty" sokel:"page_size,optional" label:"Page size"`
	Radius    *int   `json:"radius,omitempty" sokel:"radius,optional" label:"Search radius in meters"`
	Sort      string `json:"sort,omitempty" sokel:"sort,optional" label:"Sort rule"`
}
type ReverseOptions struct {
	Radius *int `json:"radius,omitempty" sokel:"radius,optional" label:"Search radius in meters"`
}
type RouteOptions struct {
	Strategy *int `json:"strategy,omitempty" sokel:"strategy,optional" label:"Driving strategy"`
}
type RouteStep struct {
	Instruction string `json:"instruction" sokel:"instruction" label:"Instruction"`
	Road        string `json:"road" sokel:"road" label:"Road"`
	Distance    string `json:"distance" sokel:"distance_m" label:"Distance in meters"`
	Duration    string `json:"duration" sokel:"duration_s" label:"Duration in seconds"`
	Polyline    string `json:"polyline" sokel:"polyline" label:"Polyline"`
}
type Route struct {
	Distance string      `json:"distance" sokel:"distance_m" label:"Distance in meters"`
	Duration string      `json:"duration" sokel:"duration_s" label:"Duration in seconds"`
	Tolls    string      `json:"tolls" sokel:"tolls_yuan" label:"Tolls in yuan"`
	Steps    []RouteStep `json:"steps" sokel:"steps" label:"Route steps"`
}
type Weather struct {
	Province      string `json:"province" sokel:"province" label:"Province"`
	City          string `json:"city" sokel:"city" label:"City"`
	Adcode        string `json:"adcode" sokel:"adcode" label:"Administrative code"`
	Weather       string `json:"weather" sokel:"weather" label:"Weather"`
	Temperature   string `json:"temperature" sokel:"temperature_c" label:"Temperature in Celsius"`
	Humidity      string `json:"humidity" sokel:"humidity_percent" label:"Humidity percent"`
	WindDirection string `json:"winddirection" sokel:"wind_direction" label:"Wind direction"`
	WindPower     string `json:"windpower" sokel:"wind_power" label:"Wind force"`
	ReportTime    string `json:"reporttime" sokel:"reported_at" label:"Reported at"`
}
type District struct {
	Name         string `json:"name" sokel:"name" label:"Name"`
	Adcode       string `json:"adcode" sokel:"adcode" label:"Administrative code"`
	Citycode     string `json:"citycode" sokel:"citycode" label:"City code"`
	Center       string `json:"center" sokel:"center" label:"Center coordinates"`
	Level        string `json:"level" sokel:"level" label:"Administrative level"`
	ParentAdcode string `json:"parent_adcode" sokel:"parent_adcode" label:"Parent administrative code"`
}
type DistrictOptions struct {
	Depth *int `json:"depth,omitempty" sokel:"depth,optional" label:"Child levels"`
	Page  *int `json:"page,omitempty" sokel:"page,optional" label:"Page number"`
}
type GeocodeAddress struct{}

func (GeocodeAddress) Meta() contract.Meta {
	return contract.Meta{ID: "geocode_address", Label: "Geocode an address", Desc: "", TimeoutSec: 60}
}
func (GeocodeAddress) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("address").Label("Address"), field.String("city").Label("City").Optional()}
}
func (GeocodeAddress) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("matches", []Geocode{}).Label("Address matches")}
}

type ReverseGeocode struct{}

func (ReverseGeocode) Meta() contract.Meta {
	return contract.Meta{ID: "reverse_geocode", Label: "Reverse geocode coordinates", Desc: "", TimeoutSec: 60}
}
func (ReverseGeocode) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("location").Label("Longitude and latitude"), field.Json("options", ReverseOptions{}).Label("Reverse geocoding options").Optional()}
}
func (ReverseGeocode) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Json("address", Geocode{}).Label("Address")}
}

type SearchPlaces struct{}

func (SearchPlaces) Meta() contract.Meta {
	return contract.Meta{ID: "search_places", Label: "Search places", Desc: "", TimeoutSec: 60}
}
func (SearchPlaces) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("query").Label("Search query"), field.Json("options", SearchOptions{}).Label("Search options").Optional()}
}
func (SearchPlaces) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("places", []Place{}).Label("Places"), field.Int("returned_count").Label("Returned count"), field.Int("next_page").Label("Next page"), field.Bool("has_more").Label("More results"), field.Bool("page_limit_reached").Label("Page limit reached")}
}

type SearchNearby struct{}

func (SearchNearby) Meta() contract.Meta {
	return contract.Meta{ID: "search_nearby", Label: "Search nearby places", Desc: "", TimeoutSec: 60}
}
func (SearchNearby) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("location").Label("Longitude and latitude"), field.String("query").Label("Search query").Optional(), field.Json("options", NearbyOptions{}).Label("Nearby search options").Optional()}
}
func (SearchNearby) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("places", []Place{}).Label("Places"), field.Int("returned_count").Label("Returned count"), field.Int("next_page").Label("Next page"), field.Bool("has_more").Label("More results"), field.Bool("page_limit_reached").Label("Page limit reached")}
}

type GetPlace struct{}

func (GetPlace) Meta() contract.Meta {
	return contract.Meta{ID: "get_place", Label: "Get place", Desc: "", TimeoutSec: 60}
}
func (GetPlace) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("place_id").Label("Place ID")}
}
func (GetPlace) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("places", []Place{}).Label("Places")}
}

type PlanRoute struct{}

func (PlanRoute) Meta() contract.Meta {
	return contract.Meta{ID: "plan_route", Label: "Plan a route", Desc: "", TimeoutSec: 60}
}
func (PlanRoute) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("mode").Label("Travel mode"), field.String("origin").Label("Origin coordinates"), field.String("destination").Label("Destination coordinates"), field.Json("options", RouteOptions{}).Label("Route options").Optional()}
}
func (PlanRoute) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("routes", []Route{}).Label("Routes")}
}

type GetWeather struct{}

func (GetWeather) Meta() contract.Meta {
	return contract.Meta{ID: "get_weather", Label: "Get current weather", Desc: "", TimeoutSec: 60}
}
func (GetWeather) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("adcode").Label("Administrative code")}
}
func (GetWeather) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("weather", []Weather{}).Label("Weather observations")}
}

type ListDistricts struct{}

func (ListDistricts) Meta() contract.Meta {
	return contract.Meta{ID: "list_districts", Label: "Find administrative districts", Desc: "", TimeoutSec: 60}
}
func (ListDistricts) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("query").Label("District name or code"), field.Json("options", DistrictOptions{}).Label("District query options").Optional()}
}
func (ListDistricts) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("districts", []District{}).Label("Districts"), field.Int("next_page").Label("Next page"), field.Bool("has_more").Label("More results")}
}

type VerifyCredential struct{}

func (VerifyCredential) Meta() contract.Meta {
	return contract.Meta{ID: "verify_credential", Label: "Verify key with a quota-consuming lookup", Desc: "Performs one geocoding request; no automatic health check is registered", TimeoutSec: 60}
}
func (VerifyCredential) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("address").Label("Probe address")}
}
func (VerifyCredential) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("ok").Label("Available"), field.String("message").Label("Message")}
}

type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{field.Secret("api_key").Label("Web Service API key"), field.Secret("signing_secret").Label("Optional signing secret").Optional().Desc("Only for keys with digital signatures enabled")}
}
