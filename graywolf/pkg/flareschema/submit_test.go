package flareschema

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSubmitResponseJSONShape(t *testing.T) {
	in := SubmitResponse{
		FlareID:       "f0b3c4d2-cafe-4f00-9900-deadbeefcafe",
		PortalToken:   "p_abcdefABCDEF1234567890",
		PortalURL:     "https://flare.nw5w.com/f/p_abcdefABCDEF1234567890",
		SchemaVersion: 1,
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, want := range []string{
		`"flare_id":"f0b3c4d2-cafe-4f00-9900-deadbeefcafe"`,
		`"portal_token":"p_abcdefABCDEF1234567890"`,
		`"portal_url":"https://flare.nw5w.com/f/p_abcdefABCDEF1234567890"`,
		`"schema_version":1`,
	} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("got %s, missing %s", b, want)
		}
	}
}

func TestSubmitResponseRoundTrip(t *testing.T) {
	in := SubmitResponse{FlareID: "id-1", PortalToken: "tok", PortalURL: "https://x", SchemaVersion: 1}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out SubmitResponse
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out != in {
		t.Fatalf("round trip: in=%+v out=%+v", in, out)
	}
}
