package flareschema

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestConfigItemRoundTrip(t *testing.T) {
	in := ConfigItem{Key: "ptt.device", Value: "/dev/ttyUSB0"}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(b) != `{"key":"ptt.device","value":"/dev/ttyUSB0"}` {
		t.Fatalf("got %s", b)
	}
	var out ConfigItem
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out != in {
		t.Fatalf("round trip: in=%+v out=%+v", in, out)
	}
}

func TestConfigSectionOrderPreserved(t *testing.T) {
	s := ConfigSection{
		Items: []ConfigItem{
			{Key: "ptt.device", Value: "/dev/ttyUSB0"},
			{Key: "aprs.callsign", Value: "N0CALL"},
			{Key: "aprs.password", Value: "[REDACTED]"},
		},
	}
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got ConfigSection
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Items) != 3 {
		t.Fatalf("len = %d, want 3", len(got.Items))
	}
	if got.Items[0].Key != "ptt.device" || got.Items[2].Key != "aprs.password" {
		t.Fatalf("order not preserved: %+v", got.Items)
	}
}

func TestConfigSectionEmptyIssuesOmitted(t *testing.T) {
	s := ConfigSection{Items: []ConfigItem{{Key: "k", Value: "v"}}}
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// "issues" must be absent when empty (omitempty contract).
	if strings.Contains(string(b), `"issues"`) {
		t.Fatalf("got %s; issues should be omitempty when empty", b)
	}
}
