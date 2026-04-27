package flareschema

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSystemFullJSON(t *testing.T) {
	s := System{
		OS:               "linux",
		OSPretty:         "Debian GNU/Linux 12 (bookworm)",
		Kernel:           "6.1.0-13-arm64",
		Arch:             "arm64",
		IsRaspberryPi:    true,
		PiModel:          "Raspberry Pi 4 Model B Rev 1.4",
		Groups:           []string{"audio", "dialout", "gpio"},
		NTPSynchronized:  true,
		UdevRulesPresent: []string{"99-graywolf.rules"},
		NetworkInterfaces: []NetworkInterface{
			{Name: "wlan0", MACOUI: "b8:27:eb", Up: true},
		},
	}
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, want := range []string{
		`"os":"linux"`,
		`"os_pretty":"Debian GNU/Linux 12 (bookworm)"`,
		`"kernel":"6.1.0-13-arm64"`,
		`"arch":"arm64"`,
		`"is_raspberry_pi":true`,
		`"pi_model":"Raspberry Pi 4 Model B Rev 1.4"`,
		`"groups":["audio","dialout","gpio"]`,
		`"ntp_synchronized":true`,
		`"udev_rules_present":["99-graywolf.rules"]`,
		`"name":"wlan0"`,
		`"mac_oui":"b8:27:eb"`,
		`"up":true`,
	} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("got %s, missing %s", b, want)
		}
	}
}

func TestSystemRoundTrip(t *testing.T) {
	in := System{OS: "darwin", Arch: "arm64", Issues: []CollectorIssue{{Kind: "parse_failed", Message: "sw_vers parse error"}}}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out System
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.OS != in.OS || out.Arch != in.Arch || len(out.Issues) != 1 || out.Issues[0].Kind != "parse_failed" {
		t.Fatalf("round trip mismatch: %+v vs %+v", in, out)
	}
}

func TestServiceStatusJSON(t *testing.T) {
	s := ServiceStatus{
		Manager:      "systemd",
		Unit:         "graywolf.service",
		IsActive:     true,
		IsFailed:     false,
		RestartCount: 2,
	}
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, want := range []string{
		`"manager":"systemd"`,
		`"unit":"graywolf.service"`,
		`"is_active":true`,
		`"is_failed":false`,
		`"restart_count":2`,
	} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("got %s, missing %s", b, want)
		}
	}
}
