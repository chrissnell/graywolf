package ax25termws

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestEnvelopeRoundTripData(t *testing.T) {
	in := Envelope{Kind: KindData, Data: []byte{0x01, 0xff, 0x42, 0x00, 0x7f}}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out Envelope
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out.Kind != KindData {
		t.Fatalf("kind: %q", out.Kind)
	}
	if !bytes.Equal(in.Data, out.Data) {
		t.Fatalf("data mismatch: %x vs %x", in.Data, out.Data)
	}
}

func TestEnvelopeRoundTripConnect(t *testing.T) {
	in := Envelope{
		Kind: KindConnect,
		Connect: &ConnectArgs{
			ChannelID: 7,
			LocalCall: "K0SWE",
			LocalSSID: 1,
			DestCall:  "W1AW",
			DestSSID:  0,
			Via:       []string{"WIDE2-1", "WIDE1-1"},
			Mod128:    true,
			Paclen:    256,
			Maxframe:  4,
			T1MS:      4000,
			N2:        10,
			Backoff:   "exponential",
		},
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out Envelope
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out.Connect == nil {
		t.Fatal("connect nil")
	}
	if out.Connect.ChannelID != 7 || out.Connect.DestCall != "W1AW" || !out.Connect.Mod128 || len(out.Connect.Via) != 2 {
		t.Fatalf("connect mismatch: %+v", out.Connect)
	}
}

func TestEnvelopeRoundTripState(t *testing.T) {
	in := Envelope{Kind: KindState, State: &StatePayload{Name: "CONNECTED", Reason: "UA received"}}
	b, _ := json.Marshal(in)
	var out Envelope
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out.State == nil || out.State.Name != "CONNECTED" || out.State.Reason != "UA received" {
		t.Fatalf("state mismatch: %+v", out.State)
	}
}

func TestEnvelopeRoundTripStats(t *testing.T) {
	in := Envelope{Kind: KindLinkStats, Stats: &StatsPayload{
		State: "CONNECTED", VS: 3, VR: 5, VA: 2, RC: 1, FramesTX: 17, BytesRX: 9001, RTTMS: 850,
	}}
	b, _ := json.Marshal(in)
	var out Envelope
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out.Stats == nil || out.Stats.VS != 3 || out.Stats.VR != 5 || out.Stats.RTTMS != 850 || out.Stats.BytesRX != 9001 {
		t.Fatalf("stats mismatch: %+v", out.Stats)
	}
}

func TestEnvelopeRoundTripError(t *testing.T) {
	in := Envelope{Kind: KindError, Error: &ErrorPayload{Code: "frmr", Message: "bad N(R)"}}
	b, _ := json.Marshal(in)
	var out Envelope
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out.Error == nil || out.Error.Code != "frmr" || out.Error.Message != "bad N(R)" {
		t.Fatalf("error mismatch: %+v", out.Error)
	}
}
