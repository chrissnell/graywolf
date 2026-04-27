package pttdevice

import "testing"

func TestLookupUSB(t *testing.T) {
	cases := []struct {
		name          string
		vid, pid      string
		wantName      string
		wantHasCM108  bool
		wantLikelyPTT bool
		wantZero      bool
	}{
		{
			name:         "specific PID wins over vendor-only fallback",
			vid:          "0d8c", pid: "000c",
			wantName:     "CM108 USB Audio (GPIO PTT capable)",
			wantHasCM108: true,
			// LikelyPTT=false: CM108 devices are recommended via HID, not serial.
		},
		{
			name:         "unlisted 0d8c PID falls back to vendor entry",
			vid:          "0d8c", pid: "ffff",
			wantName:     "C-Media CM108-family USB Audio",
			wantHasCM108: true,
		},
		{
			name:         "0c76 vendor-only fallback covers all products",
			vid:          "0c76", pid: "161e",
			wantName:     "SSS USB Audio (CM108-compatible PTT)",
			wantHasCM108: true,
		},
		{
			name:         "AIOC matched by specific VID:PID",
			vid:          "1209", pid: "7388",
			wantName:     "AIOC All-In-One-Cable (CM108-compatible PTT)",
			wantHasCM108: true,
		},
		{
			name:          "CH340 is a likely PTT serial chipset",
			vid:           "1a86", pid: "7523",
			wantName:      "CH340 USB-Serial (Digirig, Mobilinkd, generic)",
			wantLikelyPTT: true,
		},
		{
			name:          "FTDI FT232R is a likely PTT serial chipset",
			vid:           "0403", pid: "6001",
			wantName:      "FTDI FT232R USB-Serial",
			wantLikelyPTT: true,
		},
		{
			name:          "CP2102 is a likely PTT serial chipset",
			vid:           "10c4", pid: "ea60",
			wantName:      "CP2102 USB-Serial (SignaLink, Digirig)",
			wantLikelyPTT: true,
		},
		{
			name:     "Arduino Uno is known but not PTT",
			vid:      "2341", pid: "0001",
			wantName: "Arduino Uno",
			// LikelyPTT=false: dev board, not a ham interface.
		},
		{
			name:     "unknown vendor returns zero value",
			vid:      "ffff", pid: "ffff",
			wantZero: true,
		},
		{
			name:         "uppercase VID matches lowercase table entry",
			vid:          "0D8C", pid: "000C",
			wantName:     "CM108 USB Audio (GPIO PTT capable)",
			wantHasCM108: true,
		},
		{
			name:         "uppercase VID still hits vendor fallback",
			vid:          "0D8C", pid: "FFFF",
			wantName:     "C-Media CM108-family USB Audio",
			wantHasCM108: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := lookupUSB(tc.vid, tc.pid)
			if tc.wantZero {
				if got != (usbDevice{}) {
					t.Errorf("lookupUSB(%q,%q) = %+v, want zero value", tc.vid, tc.pid, got)
				}
				return
			}
			if got.Name != tc.wantName {
				t.Errorf("Name = %q, want %q", got.Name, tc.wantName)
			}
			if got.HasCM108 != tc.wantHasCM108 {
				t.Errorf("HasCM108 = %v, want %v", got.HasCM108, tc.wantHasCM108)
			}
			if got.LikelyPTT != tc.wantLikelyPTT {
				t.Errorf("LikelyPTT = %v, want %v", got.LikelyPTT, tc.wantLikelyPTT)
			}
		})
	}
}

func TestLookupUSBPrefersSpecificRegardlessOfOrder(t *testing.T) {
	// Verify lookupUSB's specific-wins-over-fallback contract holds even if
	// the fallback precedes a specific entry in the slice. The code scans
	// the whole slice before falling back, so order shouldn't matter.
	orig := knownUSBDevices
	t.Cleanup(func() { knownUSBDevices = orig })

	knownUSBDevices = []usbDevice{
		{VID: "abcd", PID: "", Name: "fallback"},
		{VID: "abcd", PID: "0001", Name: "specific"},
	}

	if got := lookupUSB("abcd", "0001").Name; got != "specific" {
		t.Errorf("got %q, want %q — specific PID must beat fallback even if listed after", got, "specific")
	}
	if got := lookupUSB("abcd", "9999").Name; got != "fallback" {
		t.Errorf("got %q, want %q — unlisted PID must hit fallback", got, "fallback")
	}
}

func TestIsCM108Compatible(t *testing.T) {
	cases := []struct {
		vid, pid string
		want     bool
	}{
		{"0d8c", "000c", true},   // specific CM108
		{"0d8c", "ffff", true},   // vendor fallback: any 0d8c is CM108
		{"0c76", "0001", true},   // SSS vendor fallback
		{"1209", "7388", true},   // AIOC
		{"1a86", "7523", false},  // CH340: known, not CM108
		{"0403", "6001", false},  // FTDI: known, not CM108
		{"ffff", "ffff", false},  // unknown
	}
	for _, tc := range cases {
		if got := isCM108Compatible(tc.vid, tc.pid); got != tc.want {
			t.Errorf("isCM108Compatible(%q,%q) = %v, want %v", tc.vid, tc.pid, got, tc.want)
		}
	}
}

func TestUSBDeviceName(t *testing.T) {
	if got := usbDeviceName("0d8c", "000c"); got != "CM108 USB Audio (GPIO PTT capable)" {
		t.Errorf("got %q", got)
	}
	if got := usbDeviceName("ffff", "ffff"); got != "" {
		t.Errorf("unknown VID:PID should return empty string, got %q", got)
	}
}
