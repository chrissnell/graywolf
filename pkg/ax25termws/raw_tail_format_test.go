package ax25termws

import (
	"strings"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/aprs"
	"github.com/chrissnell/graywolf/pkg/ax25"
	"github.com/chrissnell/graywolf/pkg/packetlog"
)

// TestRawEntryToEnvelope_TNC2FromAX25Frame covers the happy path: a
// well-formed AX.25 frame in Entry.Raw decodes via ax25.Decode and
// renders to a TNC2-style monitor line ("SRC>DEST,PATH:info"). No
// control bytes, no high bytes -- the operator's xterm renders the
// line verbatim.
func TestRawEntryToEnvelope_TNC2FromAX25Frame(t *testing.T) {
	src, _ := ax25.ParseAddress("K0SWE-9")
	dst, _ := ax25.ParseAddress("APRS")
	digi, _ := ax25.ParseAddress("WIDE2-1")
	f := &ax25.Frame{
		Source:  src,
		Dest:    dst,
		Path:    []ax25.Address{digi},
		Control: 0x03,
		PID:     0xF0,
		Info:    []byte("!4138.23N/11124.94W>test"),
	}
	raw, err := f.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	out := rawEntryToEnvelope(packetlog.Entry{
		Timestamp: time.Now(),
		Source:    "rf",
		Type:      "ui",
		Raw:       raw,
	})
	if out == nil {
		t.Fatalf("nil envelope")
	}
	if !strings.HasPrefix(out.Raw, "K0SWE-9>APRS") {
		t.Fatalf("expected TNC2 prefix, got %q", out.Raw)
	}
	if !strings.Contains(out.Raw, ":!4138.23N/11124.94W>test") {
		t.Fatalf("expected info field rendered, got %q", out.Raw)
	}
}

// TestRawEntryToEnvelope_SanitizesNonPrintable covers the Mic-E /
// compressed-position case: AX.25 info field contains bytes that
// would corrupt an xterm display (bare control codes, 8-bit binary).
// formatTNC2 must replace them with '?' so the line is safe to write.
func TestRawEntryToEnvelope_SanitizesNonPrintable(t *testing.T) {
	src, _ := ax25.ParseAddress("NW5W-5")
	dst, _ := ax25.ParseAddress("APRS")
	// Synthetic Mic-E-ish payload: high byte, escape, NUL, DEL.
	info := []byte{'\'', 0xa0, 0x1b, 0x00, 0x7f, ' ', '!', 'S'}
	f := &ax25.Frame{
		Source:  src,
		Dest:    dst,
		Control: 0x03,
		PID:     0xF0,
		Info:    info,
	}
	raw, err := f.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	out := rawEntryToEnvelope(packetlog.Entry{
		Timestamp: time.Now(),
		Source:    "rf",
		Raw:       raw,
	})
	if out == nil {
		t.Fatalf("nil envelope")
	}
	for i := 0; i < len(out.Raw); i++ {
		c := out.Raw[i]
		if c < 0x20 || c == 0x7f || c >= 0x80 {
			t.Fatalf("byte 0x%02x at %d leaked through sanitizer: %q", c, i, out.Raw)
		}
	}
	if !strings.HasPrefix(out.Raw, "NW5W-5>APRS") {
		t.Fatalf("expected NW5W-5 prefix, got %q", out.Raw)
	}
	if !strings.Contains(out.Raw, "!S") {
		t.Fatalf("expected printable tail kept, got %q", out.Raw)
	}
}

// TestRawEntryToEnvelope_FromOnDecodedFallback covers the path where
// the AX.25 frame failed to decode but the Decoded APRS struct is
// available -- still produces a printable TNC2 line.
func TestRawEntryToEnvelope_FromOnDecodedFallback(t *testing.T) {
	out := rawEntryToEnvelope(packetlog.Entry{
		Timestamp: time.Now(),
		Source:    "is",
		// Raw nil; force the decoded fallback.
		Decoded: &aprs.DecodedAPRSPacket{
			Source:  "K0SWE-9",
			Dest:    "APRS",
			Path:    []string{"TCPIP*", "qAC", "FOURTH"},
			Comment: "test comment",
		},
	})
	if out == nil {
		t.Fatalf("nil envelope")
	}
	want := "K0SWE-9>APRS,TCPIP*,qAC,FOURTH:test comment"
	if out.Raw != want {
		t.Fatalf("expected %q, got %q", want, out.Raw)
	}
	if out.From != "K0SWE-9" {
		t.Fatalf("expected From=K0SWE-9, got %q", out.From)
	}
}

// TestSanitizeForTerminal_DropsCRLF covers the framing rule: \r and
// \n are dropped, not replaced with '?'. The monitor session adds its
// own CRLF per line; preserving in-line newlines would corrupt the
// xterm grid.
func TestSanitizeForTerminal_DropsCRLF(t *testing.T) {
	in := "abc\r\ndef\nghi\r"
	got := sanitizeForTerminal(in)
	want := "abcdefghi"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}
