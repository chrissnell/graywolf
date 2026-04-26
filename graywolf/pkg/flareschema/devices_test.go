package flareschema

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPTTCandidateJSON(t *testing.T) {
	c := PTTCandidate{
		Kind:        "serial",
		Path:        "/dev/ttyUSB0",
		Vendor:      "0403",
		Product:     "6001",
		Description: "FT232R USB UART",
		Permissions: "crw-rw----",
		Owner:       "root",
		Group:       "dialout",
		Accessible:  true,
	}
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, want := range []string{
		`"kind":"serial"`,
		`"path":"/dev/ttyUSB0"`,
		`"vendor":"0403"`,
		`"product":"6001"`,
		`"description":"FT232R USB UART"`,
		`"permissions":"crw-rw----"`,
		`"owner":"root"`,
		`"group":"dialout"`,
		`"accessible":true`,
	} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("got %s, missing %s", b, want)
		}
	}
}

func TestPTTSectionWithIssues(t *testing.T) {
	s := PTTSection{
		Candidates: []PTTCandidate{{Kind: "serial", Path: "/dev/ttyUSB0"}},
		Issues:     []CollectorIssue{{Kind: "permission_denied", Path: "/sys/class/gpio/export"}},
	}
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(b), `"candidates":[`) || !strings.Contains(string(b), `"issues":[`) {
		t.Fatalf("got %s; expected both candidates and issues arrays", b)
	}
}

func TestGPSCandidateJSON(t *testing.T) {
	c := GPSCandidate{
		Kind:       "gpsd_socket",
		Path:       "/var/run/gpsd.sock",
		Reachable:  true,
		Accessible: true,
	}
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, want := range []string{
		`"kind":"gpsd_socket"`,
		`"path":"/var/run/gpsd.sock"`,
		`"reachable":true`,
		`"accessible":true`,
	} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("got %s, missing %s", b, want)
		}
	}
}
