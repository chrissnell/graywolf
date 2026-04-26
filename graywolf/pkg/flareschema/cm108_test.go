package flareschema

import (
	"encoding/json"
	"strings"
	"testing"
)

// The shape of a single CM108 device must match what enumerate_cm108()
// in graywolf-modem/src/cm108.rs already emits today. Keep this test
// in sync with that file's Cm108Device struct.
func TestCM108DeviceMatchesExistingModemOutput(t *testing.T) {
	d := CM108Device{
		Path:        "0001:0008:03",
		Vendor:      "0d8c",
		Product:     "0012",
		Description: "CM108 Audio Controller",
	}
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"path":"0001:0008:03","vendor":"0d8c","product":"0012","description":"CM108 Audio Controller"}`
	if string(b) != want {
		t.Fatalf("got %s\nwant %s", b, want)
	}
}

func TestCM108DevicesUnmarshalArrayShape(t *testing.T) {
	// graywolf-modem --list-cm108 today emits a JSON array, not an
	// object. The flare collector wraps it into CM108Devices on the Go
	// side. Confirm we can drive the wrap from the modem-shaped array.
	raw := []byte(`[{"path":"a","vendor":"0d8c","product":"0012","description":"x"}]`)
	var arr []CM108Device
	if err := json.Unmarshal(raw, &arr); err != nil {
		t.Fatalf("unmarshal array: %v", err)
	}
	if len(arr) != 1 || arr[0].Path != "a" {
		t.Fatalf("got %+v", arr)
	}
	wrap := CM108Devices{Devices: arr}
	out, err := json.Marshal(wrap)
	if err != nil {
		t.Fatalf("marshal wrap: %v", err)
	}
	if !strings.Contains(string(out), `"devices":[`) {
		t.Fatalf("got %s; expected wrap to expose a devices array", out)
	}
}
