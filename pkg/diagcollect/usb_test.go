package diagcollect

import (
	"strings"
	"testing"

	"github.com/chrissnell/graywolf/pkg/flareschema"
)

func TestCollectUSB_HappyPath(t *testing.T) {
	canned := []byte(`{"devices":[{"bus_number":1,"vendor_id":"0d8c","product_id":"0012"}]}`)
	got := collectUSBWith(fakeRunner{out: canned}, "/fake/modem")
	if len(got.Devices) != 1 {
		t.Fatalf("want 1 device, got %d", len(got.Devices))
	}
	if got.Devices[0].VendorID != "0d8c" {
		t.Fatalf("vendor_id = %q", got.Devices[0].VendorID)
	}
}

func TestCollectUSB_RunnerIssue(t *testing.T) {
	got := collectUSBWith(fakeRunner{
		issue: &flareschema.CollectorIssue{Kind: "modem_failed", Message: "exit=2"},
	}, "/fake/modem")
	if len(got.Issues) != 1 || got.Issues[0].Kind != "modem_failed" {
		t.Fatalf("issues = %+v", got.Issues)
	}
}

func TestCollectUSB_MalformedJSON(t *testing.T) {
	got := collectUSBWith(fakeRunner{out: []byte(`not json`)}, "/fake/modem")
	if len(got.Issues) != 1 || got.Issues[0].Kind != "usb_decode_failed" {
		t.Fatalf("issues = %+v", got.Issues)
	}
	if !strings.Contains(got.Issues[0].Message, "not json") {
		t.Fatalf("message should preserve stdout snippet: %s", got.Issues[0].Message)
	}
}
