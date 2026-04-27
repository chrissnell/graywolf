package flareschema

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestFlareTopLevelTags(t *testing.T) {
	f := BuildSampleFlare()
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, want := range []string{
		`"schema_version": 1`,
		`"user":`,
		`"meta":`,
		`"config":`,
		`"system":`,
		`"service_status":`,
		`"ptt":`,
		`"gps":`,
		`"audio_devices":`,
		`"usb_topology":`,
		`"cm108":`,
		`"logs":`,
	} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("missing top-level field %s in:\n%s", want, b)
		}
	}
}

func TestFlareRoundTrip(t *testing.T) {
	in := BuildSampleFlare()
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out Flare
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("round trip lost data:\nin:  %+v\nout: %+v", in, out)
	}
}

func TestFlareSchemaVersionFieldEqualsConst(t *testing.T) {
	f := BuildSampleFlare()
	if f.SchemaVersion != SchemaVersion {
		t.Fatalf("BuildSampleFlare().SchemaVersion = %d, want %d", f.SchemaVersion, SchemaVersion)
	}
}
