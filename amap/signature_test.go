package main

import (
	"context"
	"net/http"
	"testing"
)

func TestSignatureUsesRawSortedValuesBeforeURLEncoding(t *testing.T) {
	a := fakeAPI(t, func(r *http.Request) string {
		q := r.URL.Query()
		if q.Get("sig") != "f8206e714478cde5e62aedf1567f5e60" || q.Get("address") != "A+B 北京" || q.Get("output") != "JSON" {
			t.Fatal("signature or encoding does not match known vector")
		}
		if q.Has("signing_secret") {
			t.Fatal("signing secret leaked")
		}
		return `{"status":"1","infocode":"10000","geocodes":[]}`
	})
	a.cred.SigningSecret = "sign-secret"
	if _, e := a.GeocodeAddress(context.Background(), &GeocodeAddressIn{Address: "A+B 北京"}); e != nil {
		t.Fatal(e)
	}
}
