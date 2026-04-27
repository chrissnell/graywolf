package flareschema

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestLogEntryShape(t *testing.T) {
	e := LogEntry{
		TsNs:      1714069200000000000,
		Level:     "INFO",
		Component: "ptt",
		Msg:       "asserted PTT",
		Attrs:     map[string]any{"device": "/dev/ttyUSB0"},
	}
	b, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, want := range []string{
		`"ts_ns":1714069200000000000`,
		`"level":"INFO"`,
		`"component":"ptt"`,
		`"msg":"asserted PTT"`,
		`"attrs":{"device":"/dev/ttyUSB0"}`,
	} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("got %s, missing %s", b, want)
		}
	}
}

func TestLogEntryNoAttrsOmitted(t *testing.T) {
	e := LogEntry{TsNs: 1, Level: "INFO", Component: "boot", Msg: "started"}
	b, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(b), `"attrs"`) {
		t.Fatalf("got %s; attrs should be omitempty when nil", b)
	}
}

func TestLogsSectionRoundTrip(t *testing.T) {
	in := LogsSection{
		Source:  "graywolf-logs.db",
		Entries: []LogEntry{{TsNs: 1, Level: "INFO", Component: "boot", Msg: "started"}},
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out LogsSection
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Source != in.Source || len(out.Entries) != 1 {
		t.Fatalf("round trip mismatch: in=%+v out=%+v", in, out)
	}
}
