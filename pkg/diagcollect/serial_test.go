package diagcollect

import (
	"testing"

	"github.com/chrissnell/graywolf/pkg/pttdevice"
)

func TestPttDeviceTypeMapping(t *testing.T) {
	cases := []struct {
		kind, want string
	}{
		{"serial", "serial"},
		{"gpio", "gpio_chip"},
		{"cm108", "cm108_hid"},
		{"unknown_kind", "unknown_kind"}, // unmapped → preserved
	}
	for _, c := range cases {
		if got := mapPttKind(c.kind); got != c.want {
			t.Fatalf("mapPttKind(%q) = %q, want %q", c.kind, got, c.want)
		}
	}
}

func TestCollectPTT_NormalisesPttDeviceFields(t *testing.T) {
	in := []pttdevice.AvailableDevice{
		{
			Path:        "/dev/ttyUSB0",
			Type:        "serial",
			Description: "FT232R USB UART",
			USBVendor:   "0403",
			USBProduct:  "6001",
		},
	}
	got := convertPTT(in)
	if len(got.Candidates) != 1 {
		t.Fatalf("len = %d, want 1", len(got.Candidates))
	}
	c := got.Candidates[0]
	if c.Kind != "serial" || c.Path != "/dev/ttyUSB0" || c.Vendor != "0403" || c.Product != "6001" {
		t.Fatalf("unexpected: %+v", c)
	}
}

func TestCollectPTT_EmptyEnumerationStillReturnsParseableSection(t *testing.T) {
	got := convertPTT(nil)
	if got.Candidates == nil {
		t.Fatal("Candidates is nil; want empty slice")
	}
}
