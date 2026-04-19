package igate

import (
	"bytes"
	"strings"
	"testing"
)

// TestWrapThirdPartyPositionPacket exercises the happy path for a plain
// position report: outer frame must be sourced from the iGate call with
// dest APGWLF, empty path, and the inner info bytes must match the
// "}…,TCPIP,IGATECALL*:<origInfo>" format exactly.
func TestWrapThirdPartyPositionPacket(t *testing.T) {
	inner, err := parseTNC2("W5ABC-7>APRS,WIDE1-1:!3725.00N/12158.00W>hi")
	if err != nil {
		t.Fatalf("parseTNC2: %v", err)
	}
	wrapped, err := wrapThirdParty(inner, "N0CALL")
	if err != nil {
		t.Fatalf("wrapThirdParty: %v", err)
	}
	if wrapped.Source.String() != "N0CALL" {
		t.Fatalf("outer source = %q, want N0CALL", wrapped.Source.String())
	}
	if wrapped.Dest.String() != "APGWLF" {
		t.Fatalf("outer dest = %q, want APGWLF", wrapped.Dest.String())
	}
	if len(wrapped.Path) != 0 {
		t.Fatalf("outer path = %v, want empty", wrapped.Path)
	}
	want := "}W5ABC-7>APRS,WIDE1-1,TCPIP,N0CALL*:!3725.00N/12158.00W>hi"
	if string(wrapped.Info) != want {
		t.Fatalf("inner info mismatch:\n got: %s\nwant: %s", wrapped.Info, want)
	}
}

// TestWrapThirdPartyMessagePacket verifies the wrap preserves the
// directed-message payload verbatim. This is the primary traffic the
// IS→RF path carries under the spec-compliant gating rules.
func TestWrapThirdPartyMessagePacket(t *testing.T) {
	inner, err := parseTNC2("W5ABC-7>APRS,WIDE1-1,qAR,T2TEXAS::N0CALL   :hello{1")
	if err != nil {
		t.Fatalf("parseTNC2: %v", err)
	}
	wrapped, err := wrapThirdParty(inner, "N0CALL")
	if err != nil {
		t.Fatalf("wrapThirdParty: %v", err)
	}
	want := "}W5ABC-7>APRS,WIDE1-1,TCPIP,N0CALL*::N0CALL   :hello{1"
	if string(wrapped.Info) != want {
		t.Fatalf("inner info mismatch:\n got: %s\nwant: %s", wrapped.Info, want)
	}
	// Outer header: verify no stray digipeater entries.
	if len(wrapped.Path) != 0 {
		t.Fatalf("expected empty outer path, got %v", wrapped.Path)
	}
}

// TestWrapThirdPartyPreservesBinaryInfo ensures an info field with a
// NUL byte (or any other non-ASCII byte) is not truncated or mangled
// by the wrapper. The info portion of APRS can carry arbitrary binary
// (telemetry, compressed position, mic-e) so byte-preservation matters.
func TestWrapThirdPartyPreservesBinaryInfo(t *testing.T) {
	// Build an inner frame directly so we can embed a NUL byte. We
	// can't route that through parseTNC2 because TNC2 is textual.
	inner, err := parseTNC2("W5ABC>APRS:hello")
	if err != nil {
		t.Fatalf("parseTNC2: %v", err)
	}
	inner.Info = []byte{'h', 0x00, 'i'}

	wrapped, err := wrapThirdParty(inner, "N0CALL")
	if err != nil {
		t.Fatalf("wrapThirdParty: %v", err)
	}
	// Expect "}W5ABC>APRS,TCPIP,N0CALL*:" followed by 'h', 0x00, 'i'.
	prefix := []byte("}W5ABC>APRS,TCPIP,N0CALL*:")
	if !bytes.HasPrefix(wrapped.Info, prefix) {
		t.Fatalf("prefix mismatch: got %q", wrapped.Info)
	}
	tail := wrapped.Info[len(prefix):]
	if !bytes.Equal(tail, []byte{'h', 0x00, 'i'}) {
		t.Fatalf("binary info not preserved: %v", tail)
	}
}

// TestWrapThirdPartyPreservesPath verifies each original path entry is
// copied into the inner header (with no H-bit '*' artifacts) so
// downstream receivers can see the full RF routing history of the
// original packet.
func TestWrapThirdPartyPreservesPath(t *testing.T) {
	inner, err := parseTNC2("W5ABC-7>APRS,WIDE1-1,WIDE2-2:!3725.00N/12158.00W>hi")
	if err != nil {
		t.Fatalf("parseTNC2: %v", err)
	}
	wrapped, err := wrapThirdParty(inner, "N0CALL")
	if err != nil {
		t.Fatalf("wrapThirdParty: %v", err)
	}
	got := string(wrapped.Info)
	if !strings.Contains(got, ",WIDE1-1,WIDE2-2,TCPIP,") {
		t.Fatalf("path not preserved in third-party info: %s", got)
	}
	// '*' markers must not appear in the inner header — the iGate
	// didn't literally repeat those digipeaters; the H-bit is a
	// transit artifact of the RF frame, not meaningful at this layer.
	if strings.Contains(got[:strings.Index(got, ":")], "*WIDE") ||
		strings.Contains(got[:strings.Index(got, ":")], "WIDE1-1*") {
		t.Fatalf("stray H-bit '*' in inner path: %s", got)
	}
}

// TestWrapThirdPartyRejectsNilFrame checks the guard clauses.
func TestWrapThirdPartyRejectsNilFrame(t *testing.T) {
	if _, err := wrapThirdParty(nil, "N0CALL"); err == nil {
		t.Fatal("expected error for nil inner frame")
	}
}

// TestWrapThirdPartyRejectsEmptyCall mirrors the nil-frame guard for
// the iGate callsign.
func TestWrapThirdPartyRejectsEmptyCall(t *testing.T) {
	inner, err := parseTNC2("W5ABC>APRS:hello")
	if err != nil {
		t.Fatalf("parseTNC2: %v", err)
	}
	if _, err := wrapThirdParty(inner, ""); err == nil {
		t.Fatal("expected error for empty igate callsign")
	}
	if _, err := wrapThirdParty(inner, "   "); err == nil {
		t.Fatal("expected error for blank igate callsign")
	}
}
