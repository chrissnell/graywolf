package flareschema

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestUSBTopologyJSONShape(t *testing.T) {
	in := USBTopology{
		Devices: []USBDevice{
			{
				BusNumber:      1,
				PortPath:       "1-1.4.2",
				VendorID:       "0d8c",
				ProductID:      "0012",
				VendorName:     "C-Media Electronics, Inc.",
				ProductName:    "CM108 Audio Controller",
				Manufacturer:   "C-Media",
				Class:          "00",
				Subclass:       "00",
				USBVersion:     "2.00",
				Speed:          "full",
				MaxPowerMA:     100,
				HubPowerSource: "bus",
			},
		},
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, want := range []string{
		`"devices":[`,
		`"bus_number":1`,
		`"port_path":"1-1.4.2"`,
		`"vendor_id":"0d8c"`,
		`"product_id":"0012"`,
		`"vendor_name":"C-Media Electronics, Inc."`,
		`"product_name":"CM108 Audio Controller"`,
		`"manufacturer":"C-Media"`,
		`"class":"00"`,
		`"subclass":"00"`,
		`"usb_version":"2.00"`,
		`"speed":"full"`,
		`"max_power_ma":100`,
		`"hub_power_source":"bus"`,
	} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("got %s, missing %s", b, want)
		}
	}
}

func TestUSBTopologyRoundTrip(t *testing.T) {
	in := USBTopology{Devices: []USBDevice{{VendorID: "0bda", ProductID: "8153"}}}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out USBTopology
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.Devices) != 1 || out.Devices[0].VendorID != "0bda" {
		t.Fatalf("round trip mismatch: %+v", out)
	}
}
