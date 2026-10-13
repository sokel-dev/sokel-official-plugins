package main

import "testing"

// mget with no ids answers empty without asking Elasticsearch: ES rejects an empty _mget with HTTP 400, which reached
// the platform as a plugin failure whenever a retrieval had nothing left to fetch.
func TestMgetWithoutIDsIsEmpty(t *testing.T) {
	out, err := opMget(nil, &MgetIn{KbID: "kb_x"})
	if err != nil {
		t.Fatalf("mget with no ids should answer empty, got error %v", err)
	}
	if out == nil || len(out.Chunks) != 0 {
		t.Fatalf("want no chunks, got %+v", out)
	}
}
