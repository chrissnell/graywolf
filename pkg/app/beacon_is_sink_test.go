package app

import (
	"errors"
	"testing"

	"github.com/chrissnell/graywolf/pkg/packetlog"
)

type fakeISLineSink struct {
	lines []string
	err   error
}

func (f *fakeISLineSink) SendLine(line string) error {
	if f.err != nil {
		return f.err
	}
	f.lines = append(f.lines, line)
	return nil
}

func TestBeaconISSink_RecordsISEntryOnSuccess(t *testing.T) {
	plog := packetlog.New(packetlog.Config{})
	fake := &fakeISLineSink{}
	w := newBeaconISSink(fake, plog)
	const line = "N0CALL-9>APGRWO,TCPIP*:!1234.56N/12345.67W-test"
	if err := w.SendLine(line); err != nil {
		t.Fatalf("SendLine: %v", err)
	}
	entries := plog.Query(packetlog.Filter{Channel: -1})
	if len(entries) != 1 {
		t.Fatalf("packet log entries = %d, want 1", len(entries))
	}
	e := entries[0]
	if e.Direction != packetlog.DirIS {
		t.Errorf("direction = %q, want IS", e.Direction)
	}
	if e.Source != "beacon" {
		t.Errorf("source = %q, want beacon", e.Source)
	}
	if e.Display != line {
		t.Errorf("display = %q, want %q", e.Display, line)
	}
}

func TestBeaconISSink_NoEntryOnError(t *testing.T) {
	plog := packetlog.New(packetlog.Config{})
	fake := &fakeISLineSink{err: errors.New("igate: not connected")}
	w := newBeaconISSink(fake, plog)
	if err := w.SendLine("X>Y,TCPIP*:!"); err == nil {
		t.Fatal("expected error from inner sink")
	}
	if n := plog.Len(); n != 0 {
		t.Fatalf("packet log entries = %d, want 0 (no entry on failed send)", n)
	}
}

func TestNewBeaconISSink_NilInnerStaysNil(t *testing.T) {
	if got := newBeaconISSink(nil, packetlog.New(packetlog.Config{})); got != nil {
		t.Fatalf("newBeaconISSink(nil) = %v, want nil", got)
	}
}
