package flareschema

import (
	"encoding/json"
	"testing"
)

func TestCollectorIssueJSONShape(t *testing.T) {
	in := CollectorIssue{
		Kind:    "permission_denied",
		Message: "open /sys/class/gpio/export: permission denied",
		Path:    "/sys/class/gpio/export",
	}
	got, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"kind":"permission_denied","message":"open /sys/class/gpio/export: permission denied","path":"/sys/class/gpio/export"}`
	if string(got) != want {
		t.Fatalf("got %s\nwant %s", got, want)
	}
}

func TestCollectorIssueOmitsEmptyPath(t *testing.T) {
	in := CollectorIssue{Kind: "modem_unavailable", Message: "exec: no such file"}
	got, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"kind":"modem_unavailable","message":"exec: no such file"}`
	if string(got) != want {
		t.Fatalf("got %s\nwant %s (path with omitempty must drop)", got, want)
	}
}
