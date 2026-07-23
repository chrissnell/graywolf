package igate

import (
	"bytes"
	"strings"
	"testing"

	"github.com/chrissnell/graywolf/pkg/ax25"
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
	wrapped, err := wrapThirdParty(inner, "KE7XYZ", nil)
	if err != nil {
		t.Fatalf("wrapThirdParty: %v", err)
	}
	if wrapped.Source.String() != "KE7XYZ" {
		t.Fatalf("outer source = %q, want KE7XYZ", wrapped.Source.String())
	}
	if wrapped.Dest.String() != "APGWLF" {
		t.Fatalf("outer dest = %q, want APGWLF", wrapped.Dest.String())
	}
	if len(wrapped.Path) != 0 {
		t.Fatalf("outer path = %v, want empty", wrapped.Path)
	}
	want := "}W5ABC-7>APRS,WIDE1-1,TCPIP,KE7XYZ*:!3725.00N/12158.00W>hi"
	if string(wrapped.Info) != want {
		t.Fatalf("inner info mismatch:\n got: %s\nwant: %s", wrapped.Info, want)
	}
}

// TestWrapThirdPartyMessagePacket verifies the wrap preserves the
// directed-message payload verbatim. This is the primary traffic the
// IS→RF path carries under the spec-compliant gating rules.
func TestWrapThirdPartyMessagePacket(t *testing.T) {
	inner, err := parseTNC2("W5ABC-7>APRS,WIDE1-1,qAR,T2TEXAS::KE7XYZ   :hello{1")
	if err != nil {
		t.Fatalf("parseTNC2: %v", err)
	}
	wrapped, err := wrapThirdParty(inner, "KE7XYZ", nil)
	if err != nil {
		t.Fatalf("wrapThirdParty: %v", err)
	}
	want := "}W5ABC-7>APRS,WIDE1-1,TCPIP,KE7XYZ*::KE7XYZ   :hello{1"
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

	wrapped, err := wrapThirdParty(inner, "KE7XYZ", nil)
	if err != nil {
		t.Fatalf("wrapThirdParty: %v", err)
	}
	// Expect "}W5ABC>APRS,TCPIP,KE7XYZ*:" followed by 'h', 0x00, 'i'.
	prefix := []byte("}W5ABC>APRS,TCPIP,KE7XYZ*:")
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
	wrapped, err := wrapThirdParty(inner, "KE7XYZ", nil)
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

// TestWrapThirdPartyStripsInboundTCPIP verifies that a TCPIP element
// already present on the inbound APRS-IS path is not re-emitted, so the
// inner header carries exactly one canonical ",TCPIP,IGATECALL*" marker.
// A duplicated "TCPIP,TCPIP" causes Kenwood radios (e.g. TH-D75) to
// silently drop the igated message (graywolf #488).
func TestWrapThirdPartyStripsInboundTCPIP(t *testing.T) {
	inner, err := parseTNC2("W5ABC-7>APZ100,TCPIP,IGATE*,qAC,SERVER::KE7XYZ   :hi{1")
	if err != nil {
		t.Fatalf("parseTNC2: %v", err)
	}
	wrapped, err := wrapThirdParty(inner, "KE7XYZ", nil)
	if err != nil {
		t.Fatalf("wrapThirdParty: %v", err)
	}
	got := string(wrapped.Info)
	want := "}W5ABC-7>APZ100,IGATE,TCPIP,KE7XYZ*::KE7XYZ   :hi{1"
	if got != want {
		t.Fatalf("inner info mismatch:\n got: %s\nwant: %s", got, want)
	}
	if strings.Contains(got, "TCPIP,TCPIP") {
		t.Fatalf("duplicate TCPIP not stripped: %s", got)
	}
}

// TestWrapThirdPartyStripsInboundTCPIPHBit ensures the strip matches a
// TCPIP element whose H-bit '*' marker is still set. The frame is built
// directly (not via parseTNC2, which clears the H-bit) so the address
// reaches wrapThirdParty with Repeated=true — its String() renders
// "TCPIP*", which would defeat a String()-based match but not the
// Call-based one this fix uses.
func TestWrapThirdPartyStripsInboundTCPIPHBit(t *testing.T) {
	src, err := ax25.ParseAddress("W5ABC-7")
	if err != nil {
		t.Fatalf("ParseAddress src: %v", err)
	}
	dest, err := ax25.ParseAddress("APZ100")
	if err != nil {
		t.Fatalf("ParseAddress dest: %v", err)
	}
	path := []ax25.Address{{Call: "TCPIP", Repeated: true}}
	inner, err := ax25.NewUIFrame(src, dest, path, []byte("!3725.00N/12158.00W>hi"))
	if err != nil {
		t.Fatalf("NewUIFrame: %v", err)
	}
	wrapped, err := wrapThirdParty(inner, "KE7XYZ", nil)
	if err != nil {
		t.Fatalf("wrapThirdParty: %v", err)
	}
	got := string(wrapped.Info)
	want := "}W5ABC-7>APZ100,TCPIP,KE7XYZ*:!3725.00N/12158.00W>hi"
	if got != want {
		t.Fatalf("inner info mismatch:\n got: %s\nwant: %s", got, want)
	}
	if strings.Contains(got, "TCPIP,TCPIP") || strings.Contains(got, "TCPIP*,") {
		t.Fatalf("inbound H-bit TCPIP not stripped: %s", got)
	}
}

// TestWrapThirdPartyStripsInboundTCPXX mirrors the TCPIP case for the
// TCPXX marker (unverified-login APRS-IS traffic).
func TestWrapThirdPartyStripsInboundTCPXX(t *testing.T) {
	inner, err := parseTNC2("W5ABC-7>APZ100,TCPXX,qAX,SERVER:!3725.00N/12158.00W>hi")
	if err != nil {
		t.Fatalf("parseTNC2: %v", err)
	}
	wrapped, err := wrapThirdParty(inner, "KE7XYZ", nil)
	if err != nil {
		t.Fatalf("wrapThirdParty: %v", err)
	}
	got := string(wrapped.Info)
	if strings.Contains(got, "TCPXX") {
		t.Fatalf("inbound TCPXX not stripped: %s", got)
	}
	want := "}W5ABC-7>APZ100,TCPIP,KE7XYZ*:!3725.00N/12158.00W>hi"
	if got != want {
		t.Fatalf("inner info mismatch:\n got: %s\nwant: %s", got, want)
	}
}

// TestWrapThirdPartyAppliesVia verifies the operator-configured IS→RF
// via-path lands on the OUTER frame (so digipeaters actually relay the
// downlink) without disturbing the inner third-party header. This is the
// fix for issue #489, where the outer path was hardcoded empty.
func TestWrapThirdPartyAppliesVia(t *testing.T) {
	inner, err := parseTNC2("W5ABC-7>APRS::KE7XYZ   :hello{1")
	if err != nil {
		t.Fatalf("parseTNC2: %v", err)
	}
	via, err := ax25.ParseVia("WIDE1-1,WIDE2-1")
	if err != nil {
		t.Fatalf("ParseVia: %v", err)
	}
	wrapped, err := wrapThirdParty(inner, "KE7XYZ", via)
	if err != nil {
		t.Fatalf("wrapThirdParty: %v", err)
	}
	if len(wrapped.Path) != 2 {
		t.Fatalf("outer path len = %d, want 2: %v", len(wrapped.Path), wrapped.Path)
	}
	if wrapped.Path[0].String() != "WIDE1-1" || wrapped.Path[1].String() != "WIDE2-1" {
		t.Fatalf("outer path = %v, want [WIDE1-1 WIDE2-1]", wrapped.Path)
	}
	// The via-path must not leak into the inner third-party header.
	want := "}W5ABC-7>APRS,TCPIP,KE7XYZ*::KE7XYZ   :hello{1"
	if string(wrapped.Info) != want {
		t.Fatalf("inner info mismatch:\n got: %s\nwant: %s", wrapped.Info, want)
	}
}

// TestWrapThirdPartyRejectsNilFrame checks the guard clauses.
func TestWrapThirdPartyRejectsNilFrame(t *testing.T) {
	if _, err := wrapThirdParty(nil, "KE7XYZ", nil); err == nil {
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
	if _, err := wrapThirdParty(inner, "", nil); err == nil {
		t.Fatal("expected error for empty igate callsign")
	}
	if _, err := wrapThirdParty(inner, "   ", nil); err == nil {
		t.Fatal("expected error for blank igate callsign")
	}
}
