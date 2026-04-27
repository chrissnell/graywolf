package aprs

import (
	"testing"

	"github.com/chrissnell/graywolf/pkg/ax25"
)

func makeRaw(t *testing.T, src, dst, info string) []byte {
	t.Helper()
	s, err := ax25.ParseAddress(src)
	if err != nil {
		t.Fatalf("ParseAddress(%q): %v", src, err)
	}
	d, err := ax25.ParseAddress(dst)
	if err != nil {
		t.Fatalf("ParseAddress(%q): %v", dst, err)
	}
	f, err := ax25.NewUIFrame(s, d, nil, []byte(info))
	if err != nil {
		t.Fatalf("NewUIFrame: %v", err)
	}
	raw, err := f.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	return raw
}

func TestDecodedAPRSPacketDedupKey(t *testing.T) {
	p1 := &DecodedAPRSPacket{
		Source: "W5ABC-7",
		Raw:    makeRaw(t, "W5ABC-7", "APRS", "!3725.00N/12158.00W>hi"),
	}
	p2 := &DecodedAPRSPacket{
		Source: "W5ABC-7",
		Raw:    makeRaw(t, "W5ABC-7", "APRS", "!3725.00N/12158.00W>hi"),
	}
	if p1.DedupKey() == "" {
		t.Fatal("non-empty info should produce non-empty key")
	}
	if p1.DedupKey() != p2.DedupKey() {
		t.Error("identical packets should produce identical keys")
	}

	// Different source, same info: distinct keys (two stations sending
	// identical text must not collapse).
	p3 := &DecodedAPRSPacket{
		Source: "W9XYZ-5",
		Raw:    makeRaw(t, "W9XYZ-5", "APRS", "!3725.00N/12158.00W>hi"),
	}
	if p1.DedupKey() == p3.DedupKey() {
		t.Error("different sources should produce different keys")
	}

	// Different info, same source: distinct keys.
	p4 := &DecodedAPRSPacket{
		Source: "W5ABC-7",
		Raw:    makeRaw(t, "W5ABC-7", "APRS", "different payload"),
	}
	if p1.DedupKey() == p4.DedupKey() {
		t.Error("different info should produce different keys")
	}

	// The path is part of the AX.25 frame but NOT part of the APRS-level
	// key. Two copies of the same payload from the same source heard
	// via different digipeater paths should collapse (the iGate wants
	// to post the first one and drop the rest).
	srcAddr, _ := ax25.ParseAddress("W5ABC-7")
	dstAddr, _ := ax25.ParseAddress("APRS")
	via1, _ := ax25.NewUIFrame(srcAddr, dstAddr, []ax25.Address{{Call: "WIDE1", SSID: 1}}, []byte("same"))
	via2, _ := ax25.NewUIFrame(srcAddr, dstAddr, []ax25.Address{{Call: "WIDE2", SSID: 2}}, []byte("same"))
	raw1, _ := via1.Encode()
	raw2, _ := via2.Encode()
	pA := &DecodedAPRSPacket{Source: "W5ABC-7", Raw: raw1}
	pB := &DecodedAPRSPacket{Source: "W5ABC-7", Raw: raw2}
	if pA.DedupKey() != pB.DedupKey() {
		t.Error("APRS dedup key should ignore the AX.25 path")
	}
}

func TestDecodedAPRSPacketDedupKeyEmpty(t *testing.T) {
	// A packet with no Raw bytes cannot produce a key; empty string
	// is the documented "do not dedup" signal.
	if got := (&DecodedAPRSPacket{Source: "N0CALL"}).DedupKey(); got != "" {
		t.Errorf("no Raw -> key should be empty, got %q", got)
	}
	// Nil receiver is safe.
	var p *DecodedAPRSPacket
	if got := p.DedupKey(); got != "" {
		t.Errorf("nil receiver -> key should be empty, got %q", got)
	}
}
