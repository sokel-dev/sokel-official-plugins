package main

import (
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
	"log"
)

func registerOperations(h plugin.Host, getAPI func(plugin.Ctx) *api) {
	OnGeocodeAddress(h, func(c plugin.Ctx, in *GeocodeAddressIn) (*GeocodeAddressOut, error) {
		return getAPI(c).GeocodeAddress(c, in)
	})
	OnGetPlace(h, func(c plugin.Ctx, in *GetPlaceIn) (*GetPlaceOut, error) { return getAPI(c).GetPlace(c, in) })
	OnGetWeather(h, func(c plugin.Ctx, in *GetWeatherIn) (*GetWeatherOut, error) { return getAPI(c).GetWeather(c, in) })
	OnListDistricts(h, func(c plugin.Ctx, in *ListDistrictsIn) (*ListDistrictsOut, error) {
		return getAPI(c).ListDistricts(c, in)
	})
	OnPlanRoute(h, func(c plugin.Ctx, in *PlanRouteIn) (*PlanRouteOut, error) { return getAPI(c).PlanRoute(c, in) })
	OnReverseGeocode(h, func(c plugin.Ctx, in *ReverseGeocodeIn) (*ReverseGeocodeOut, error) {
		return getAPI(c).ReverseGeocode(c, in)
	})
	OnSearchNearby(h, func(c plugin.Ctx, in *SearchNearbyIn) (*SearchNearbyOut, error) { return getAPI(c).SearchNearby(c, in) })
	OnSearchPlaces(h, func(c plugin.Ctx, in *SearchPlacesIn) (*SearchPlacesOut, error) { return getAPI(c).SearchPlaces(c, in) })
	OnVerifyCredential(h, func(c plugin.Ctx, in *VerifyCredentialIn) (*VerifyCredentialOut, error) {
		return getAPI(c).VerifyCredential(c, in)
	})
}
func main() {
	p := sokel.New(sokel.Config{Endpoint: sokel.EnvOr("ENDPOINT", "http://localhost:8088"), Token: sokel.Env("TOKEN"), Name: "amap"})
	p.SetVersion("0.1.0")
	p.SetDoc(usageDoc, "")
	RegisterCredential(p)
	registerOperations(p, func(c plugin.Ctx) *api { return newAPI(sokel.CredentialAs[Cred](c)) })
	if e := p.Run(); e != nil {
		log.Fatal(e)
	}
}
