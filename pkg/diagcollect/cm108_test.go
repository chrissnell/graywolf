package diagcollect

import (
	"testing"

	"github.com/chrissnell/graywolf/pkg/flareschema"
)

func TestCollectCM108_WrapsArrayShape(t *testing.T) {
	canned := []byte(`[{"path":"0001:0008:03","vendor":"0d8c","product":"0012","description":"x"}]`)
	got := collectCM108With(fakeRunner{out: canned}, "/fake/modem")
	if len(got.Devices) != 1 || got.Devices[0].Vendor != "0d8c" {
		t.Fatalf("unexpected: %+v", got)
	}
}

func TestCollectCM108_EmptyArray(t *testing.T) {
	got := collectCM108With(fakeRunner{out: []byte(`[]`)}, "/fake/modem")
	if len(got.Devices) != 0 {
		t.Fatalf("Devices = %+v, want empty", got.Devices)
	}
	if len(got.Issues) != 0 {
		t.Fatalf("issues = %+v", got.Issues)
	}
}

func TestCollectCM108_RunnerIssue(t *testing.T) {
	got := collectCM108With(fakeRunner{
		issue: &flareschema.CollectorIssue{Kind: "modem_unavailable", Message: "x"},
	}, "")
	if len(got.Issues) != 1 || got.Issues[0].Kind != "modem_unavailable" {
		t.Fatalf("issues = %+v", got.Issues)
	}
}

func TestCollectCM108_MalformedJSON(t *testing.T) {
	got := collectCM108With(fakeRunner{out: []byte(`{"oops"}`)}, "/fake/modem")
	if len(got.Issues) != 1 || got.Issues[0].Kind != "cm108_decode_failed" {
		t.Fatalf("issues = %+v", got.Issues)
	}
}
