package igate

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/chrissnell/graywolf/pkg/aprs"
	"github.com/chrissnell/graywolf/pkg/ax25"
	"github.com/chrissnell/graywolf/pkg/igate/filters"
	"github.com/chrissnell/graywolf/pkg/txgovernor"
)

// TestSourceIsOwnSSID exercises the ownership helper directly so edge
// cases (empty inputs, case/whitespace normalization, SSID asymmetry)
// are pinned without going through the full handleISLine path.
func TestSourceIsOwnSSID(t *testing.T) {
	ig, err := New(Config{Server: "127.0.0.1:1", StationCallsign: "KE7XYZ-10"})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		source string
		want   bool
	}{
		{"different SSID of same base", "KE7XYZ-7", true},
		{"base-call vs SSID-qualified iGate", "KE7XYZ", true},
		{"exact match of iGate call rejected", "KE7XYZ-10", false},
		{"case insensitive base match", "ke7xyz-7", true},
		{"leading/trailing whitespace tolerated", "  KE7XYZ-7  ", true},
		{"stranger base rejected", "W5ABC-7", false},
		{"empty source rejected", "", false},
		{"whitespace-only source rejected", "   ", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ig.sourceIsOwnSSID(c.source); got != c.want {
				t.Fatalf("sourceIsOwnSSID(%q) = %v, want %v", c.source, got, c.want)
			}
		})
	}
}

// TestSourceIsOwnSSID_NoSSIDOnIgate verifies the helper when the iGate
// is configured without an SSID: any SSID-qualified form of the base
// call is still "ours", and only the bare base call (which equals the
// iGate's exact callsign) is rejected as a self-echo.
func TestSourceIsOwnSSID_NoSSIDOnIgate(t *testing.T) {
	ig, err := New(Config{Server: "127.0.0.1:1", StationCallsign: "KE7XYZ"})
	if err != nil {
		t.Fatal(err)
	}
	if !ig.sourceIsOwnSSID("KE7XYZ-7") {
		t.Fatal("KE7XYZ-7 should be ours when iGate is plain KE7XYZ")
	}
	if ig.sourceIsOwnSSID("KE7XYZ") {
		t.Fatal("bare KE7XYZ equals iGate's exact call and must be rejected")
	}
}

// newSpecTestIgate builds an iGate with a permissive W5 filter so the
// user-level filter engine never masks the spec gate's decision in
// these tests. The heard-direct tracker is intentionally empty — each
// test records what it needs.
func newSpecTestIgate(t *testing.T, submits *int32) *Igate {
	t.Helper()
	ig, err := New(Config{
		Server:          "127.0.0.1:1",
		StationCallsign: "KE7XYZ",
		Rules: []filters.Rule{
			{ID: 1, Type: filters.TypePrefix, Pattern: "W5", Action: filters.Allow},
		},
		Governor: &stubGovernor{
			fn: func(ctx context.Context, channel uint32, frame *ax25.Frame, src txgovernor.SubmitSource) error {
				if submits != nil {
					atomic.AddInt32(submits, 1)
				}
				return nil
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return ig
}

// TestISToRF_Spec_MessageToHeardAddresseePasses is the happy path: a
// directed message addressed to a station we've heard directly on RF
// survives the spec gate, the user filter, and reaches Submit.
func TestISToRF_Spec_MessageToHeardAddresseePasses(t *testing.T) {
	var submits int32
	ig := newSpecTestIgate(t, &submits)
	ig.heard.Record("KE7XYZ")

	ig.handleISLine("W5ABC>APRS,WIDE1-1::KE7XYZ   :hello{1")

	if got := atomic.LoadInt32(&submits); got != 1 {
		t.Fatalf("expected 1 submit after spec gate, got %d", got)
	}
	if ig.Status().Downlinked != 1 {
		t.Fatalf("Downlinked = %d, want 1", ig.Status().Downlinked)
	}
}

// newSpecTestIgateOwnedFilter builds an iGate whose user filter allows
// the operator's base callsign (KE7XYZ). Used by the non-message
// ownership tests so the filter itself never rejects a packet the
// ownership rule admits.
func newSpecTestIgateOwnedFilter(t *testing.T, submits *int32) *Igate {
	t.Helper()
	ig, err := New(Config{
		Server:          "127.0.0.1:1",
		StationCallsign: "KE7XYZ",
		Rules: []filters.Rule{
			{ID: 1, Type: filters.TypePrefix, Pattern: "KE7XYZ", Action: filters.Allow},
		},
		Governor: &stubGovernor{
			fn: func(ctx context.Context, channel uint32, frame *ax25.Frame, src txgovernor.SubmitSource) error {
				if submits != nil {
					atomic.AddInt32(submits, 1)
				}
				return nil
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return ig
}

// TestISToRF_NonMessageFromOwnSSIDPasses covers the graywolf non-message
// ownership rule: a position packet sourced from another SSID of the
// operator's base callsign (KE7XYZ-7 while the iGate is KE7XYZ) is
// eligible for IS→RF. This is the internet-connected-weather-station
// use case: echo my own stuff onto local RF.
func TestISToRF_NonMessageFromOwnSSIDPasses(t *testing.T) {
	var submits int32
	ig := newSpecTestIgateOwnedFilter(t, &submits)

	ig.handleISLine("KE7XYZ-7>APRS,TCPIP*:!3725.00N/12158.00W>wx")

	if got := atomic.LoadInt32(&submits); got != 1 {
		t.Fatalf("own-SSID non-message should reach Submit, got %d", got)
	}
	if ig.Status().Downlinked != 1 {
		t.Fatalf("Downlinked = %d, want 1", ig.Status().Downlinked)
	}
}

// TestISToRF_NonMessageFromExactOwnCallRejected verifies the ownership
// rule's self-echo guard: a packet sourced from the iGate's exact
// transmitting callsign must be rejected even though it trivially
// matches the operator's base call.
func TestISToRF_NonMessageFromExactOwnCallRejected(t *testing.T) {
	var submits int32
	ig := newSpecTestIgateOwnedFilter(t, &submits)

	ig.handleISLine("KE7XYZ>APRS,TCPIP*:!3725.00N/12158.00W>self")

	if got := atomic.LoadInt32(&submits); got != 0 {
		t.Fatalf("self-sourced non-message should not reach Submit, got %d", got)
	}
	if ig.Status().Filtered != 1 {
		t.Fatalf("Filtered = %d, want 1 (self-echo rejected)", ig.Status().Filtered)
	}
}

// TestISToRF_NonMessageFromStrangerRejected: a non-message packet from
// a source whose base call doesn't match the operator's is dropped at
// the ownership gate, regardless of the user filter. This is the core
// of the new policy — "you can't IS→RF a call that's not yours".
func TestISToRF_NonMessageFromStrangerRejected(t *testing.T) {
	var submits int32
	ig := newSpecTestIgate(t, &submits)

	ig.handleISLine("W5ABC>APRS,WIDE1-1:!3725.00N/12158.00W>hi")

	if got := atomic.LoadInt32(&submits); got != 0 {
		t.Fatalf("stranger non-message should not reach Submit, got %d", got)
	}
	if ig.Status().Filtered != 1 {
		t.Fatalf("Filtered = %d, want 1 (non-owned source rejected)", ig.Status().Filtered)
	}
}

// TestISToRF_NonMessageFilterStillApplies: a packet that passes the
// ownership gate can still be rejected by the user filter. Here we
// install an iGate whose filter only allows "W5" — an KE7XYZ-7 source
// passes ownership but the filter drops it. Confirms the ownership and
// filter layers are both in play.
func TestISToRF_NonMessageFilterStillApplies(t *testing.T) {
	var submits int32
	ig := newSpecTestIgate(t, &submits)

	ig.handleISLine("KE7XYZ-7>APRS,TCPIP*:!3725.00N/12158.00W>wx")

	if got := atomic.LoadInt32(&submits); got != 0 {
		t.Fatalf("filter-rejected own-SSID packet should not reach Submit, got %d", got)
	}
	if ig.Status().Filtered != 1 {
		t.Fatalf("Filtered = %d, want 1 (user filter rejected)", ig.Status().Filtered)
	}
}

// TestISToRF_NonMessageLoopPrevented verifies loop prevention applies to
// non-message traffic even when ownership says yes: a position packet
// from our own SSID whose path already contains our call (so we already
// transmitted this) must still be dropped.
func TestISToRF_NonMessageLoopPrevented(t *testing.T) {
	var submits int32
	ig := newSpecTestIgateOwnedFilter(t, &submits)

	ig.handleISLine("KE7XYZ-7>APRS,KE7XYZ*,WIDE1-1:!3725.00N/12158.00W>hi")

	if got := atomic.LoadInt32(&submits); got != 0 {
		t.Fatalf("loop-prevention should drop own-SSID non-message too; got %d Submit calls", got)
	}
	if ig.Status().Filtered != 1 {
		t.Fatalf("Filtered = %d, want 1 (loop-prevention)", ig.Status().Filtered)
	}
}

// TestISToRF_Spec_BulletinRejected covers IsBulletin: a message to a
// BLN* addressee is a broadcast, not directed traffic; the spec gate
// must reject it even if the addressee appears in heard-direct.
func TestISToRF_Spec_BulletinRejected(t *testing.T) {
	var submits int32
	ig := newSpecTestIgate(t, &submits)
	ig.heard.Record("BLN1")

	ig.handleISLine("W5ABC>APRS,WIDE1-1::BLN1     :bulletin text")

	if got := atomic.LoadInt32(&submits); got != 0 {
		t.Fatalf("bulletin should not reach Submit, got %d", got)
	}
	if ig.Status().Filtered != 1 {
		t.Fatalf("Filtered = %d, want 1 (bulletin rejected)", ig.Status().Filtered)
	}
}

// TestISToRF_Spec_NWSRejected covers the IsNWS flag. NWS traffic is
// broadcast and must not be gated IS->RF.
func TestISToRF_Spec_NWSRejected(t *testing.T) {
	var submits int32
	ig := newSpecTestIgate(t, &submits)
	ig.heard.Record("NWS")

	ig.handleISLine("W5ABC>APRS,WIDE1-1::NWS-TEST :storm warning")

	if got := atomic.LoadInt32(&submits); got != 0 {
		t.Fatalf("NWS broadcast should not reach Submit, got %d", got)
	}
	if ig.Status().Filtered != 1 {
		t.Fatalf("Filtered = %d, want 1 (NWS rejected)", ig.Status().Filtered)
	}
}

// TestISToRF_Spec_AddresseeNotHeardRejected is the core spec rule: a
// message to a station we have NOT heard directly on RF must be
// dropped. Otherwise every iGate would blast every internet message
// onto RF for unreachable recipients.
func TestISToRF_Spec_AddresseeNotHeardRejected(t *testing.T) {
	var submits int32
	ig := newSpecTestIgate(t, &submits)
	// heard-direct empty — KE7XYZ has NOT been heard.

	ig.handleISLine("W5ABC>APRS,WIDE1-1::KE7XYZ   :hello{1")

	if got := atomic.LoadInt32(&submits); got != 0 {
		t.Fatalf("unheard-addressee message should not reach Submit, got %d", got)
	}
	if ig.Status().Filtered != 1 {
		t.Fatalf("Filtered = %d, want 1 (addressee unheard)", ig.Status().Filtered)
	}
}

// TestISToRF_Spec_LoopPreventionOwnCallInPath verifies we drop packets
// whose path already contains our callsign — this packet has already
// transited us, gating it again would create a loop.
func TestISToRF_Spec_LoopPreventionOwnCallInPath(t *testing.T) {
	var submits int32
	ig := newSpecTestIgate(t, &submits)
	ig.heard.Record("KE7XYZ")

	// Path contains KE7XYZ* (we already handled this packet) — must drop.
	ig.handleISLine("W5ABC>APRS,KE7XYZ*,WIDE1-1::KE7XYZ   :hello{1")

	if got := atomic.LoadInt32(&submits); got != 0 {
		t.Fatalf("loop-prevention should drop; got %d Submit calls", got)
	}
	if ig.Status().Filtered != 1 {
		t.Fatalf("Filtered = %d, want 1 (loop-prevention)", ig.Status().Filtered)
	}
}

// TestISToRF_Spec_EmptyAddresseeRejected: an ill-formed message with a
// whitespace-only addressee must never gate.
func TestISToRF_Spec_EmptyAddresseeRejected(t *testing.T) {
	var submits int32
	ig := newSpecTestIgate(t, &submits)
	// Seed empty-call lookup defensively (should not match anyway).
	ig.heard.Record("")

	ig.handleISLine("W5ABC>APRS,WIDE1-1::         :hello{1")

	if got := atomic.LoadInt32(&submits); got != 0 {
		t.Fatalf("empty-addressee should not reach Submit, got %d", got)
	}
	if ig.Status().Filtered != 1 {
		t.Fatalf("Filtered = %d, want 1", ig.Status().Filtered)
	}
}

// TestISToRF_Spec_WrappedAsThirdParty verifies the frame handed to
// Submit is the APRS third-party wrapper (outer source=our call,
// dest=APGWLF, info begins "}origSrc>…,TCPIP,KE7XYZ*:").
func TestISToRF_Spec_WrappedAsThirdParty(t *testing.T) {
	var capturedSrc, capturedDst string
	var capturedInfo []byte
	ig, err := New(Config{
		Server:          "127.0.0.1:1",
		StationCallsign: "KE7XYZ",
		Rules: []filters.Rule{
			{ID: 1, Type: filters.TypePrefix, Pattern: "W5", Action: filters.Allow},
		},
		Governor: &stubGovernor{
			fn: func(ctx context.Context, channel uint32, frame *ax25.Frame, src txgovernor.SubmitSource) error {
				capturedSrc = frame.Source.String()
				capturedDst = frame.Dest.String()
				capturedInfo = append([]byte(nil), frame.Info...)
				return nil
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ig.heard.Record("KE7XYZ")

	ig.handleISLine("W5ABC-7>APRS,WIDE1-1::KE7XYZ   :hi{1")

	if capturedSrc != "KE7XYZ" {
		t.Fatalf("outer source = %q, want KE7XYZ", capturedSrc)
	}
	if capturedDst != "APGWLF" {
		t.Fatalf("outer dest = %q, want APGWLF", capturedDst)
	}
	want := "}W5ABC-7>APRS,WIDE1-1,TCPIP,KE7XYZ*::KE7XYZ   :hi{1"
	if string(capturedInfo) != want {
		t.Fatalf("info mismatch:\n got: %s\nwant: %s", capturedInfo, want)
	}
}

// TestGateRFToIS_RecordsHeardDirect verifies the RF->IS path records
// every direct-RF arrival into the heard-direct tracker, even when the
// packet itself is not gated up (e.g. because NOGATE is in the path).
func TestGateRFToIS_RecordsHeardDirect(t *testing.T) {
	ig, err := New(Config{Server: "127.0.0.1:1", StationCallsign: "KE7XYZ"})
	if err != nil {
		t.Fatal(err)
	}

	// A direct-RF NOGATE packet: will not gate, but must register as heard.
	raw := buildRawFrame(t, "W5ABC-7", "APRS", []string{"NOGATE"}, "!3725.00N/12158.00W>hi")
	pkt := &aprs.DecodedAPRSPacket{
		Source: "W5ABC-7",
		Dest:   "APRS",
		Path:   []string{"NOGATE"},
		Raw:    raw,
		Type:   aprs.PacketPosition,
	}
	ig.gateRFToIS(pkt)

	if !ig.heard.HeardWithin("W5ABC-7", heardDirectTTL) {
		t.Fatal("RF arrival (even NOGATE) must populate heard-direct tracker")
	}
}

// TestGateRFToIS_SkipsHeardDirectForRepeatedPath verifies that a
// packet whose path has an '*' (some digipeater already repeated it)
// does NOT register as heard-direct. Direct RF reception is the
// precondition per the spec.
func TestGateRFToIS_SkipsHeardDirectForRepeatedPath(t *testing.T) {
	ig, err := New(Config{Server: "127.0.0.1:1", StationCallsign: "KE7XYZ"})
	if err != nil {
		t.Fatal(err)
	}
	raw := buildRawFrame(t, "W5ABC-7", "APRS", []string{"WIDE1-1"}, "!3725.00N/12158.00W>hi")
	pkt := &aprs.DecodedAPRSPacket{
		Source: "W5ABC-7",
		Dest:   "APRS",
		Path:   []string{"WIDE1-1*"}, // marked repeated
		Raw:    raw,
		Type:   aprs.PacketPosition,
	}
	ig.gateRFToIS(pkt)

	if ig.heard.HeardWithin("W5ABC-7", heardDirectTTL) {
		t.Fatal("digipeater-repeated packet must not register as heard-direct")
	}
}
