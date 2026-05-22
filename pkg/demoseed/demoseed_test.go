package demoseed

import (
	"testing"
	"time"
)

func TestStations_FreshAndInBox(t *testing.T) {
	entries := Stations()
	if len(entries) != 17 {
		t.Fatalf("want 17 demo stations, got %d", len(entries))
	}
	now := time.Now()
	for _, e := range entries {
		if e.Callsign == "" {
			t.Errorf("empty callsign in entry %q", e.Key)
		}
		if !e.HasPos {
			t.Errorf("%s missing position", e.Callsign)
		}
		// Fresh: within the last 15 minutes so the map's time-window
		// query (default 1h) always includes them.
		if now.Sub(e.Timestamp) > 15*time.Minute {
			t.Errorf("%s timestamp too old: %v", e.Callsign, e.Timestamp)
		}
		// Salt Lake bounding box.
		if e.Lat < 40.4 || e.Lat > 41.0 || e.Lon < -112.3 || e.Lon > -111.6 {
			t.Errorf("%s out of SLC box: %f,%f", e.Callsign, e.Lat, e.Lon)
		}
	}
}

func TestStatusCounters_Plausible(t *testing.T) {
	c := StatusCounters()
	if c.RxFrames == 0 || c.UptimeSeconds == 0 {
		t.Fatal("expected non-zero demo counters")
	}
}

func TestPackets_Derived(t *testing.T) {
	pkts := Packets()
	if len(pkts) != 17 {
		t.Fatalf("want 17 demo packets, got %d", len(pkts))
	}
	now := time.Now()
	for _, p := range pkts {
		if p.Display == "" {
			t.Error("empty packet display line")
		}
		if p.Direction != "RX" {
			t.Errorf("want RX direction, got %q", p.Direction)
		}
		if now.Sub(p.Timestamp) > 15*time.Minute {
			t.Errorf("packet timestamp too old: %v", p.Timestamp)
		}
	}
}

func TestAprsCoord(t *testing.T) {
	got := aprsCoord(40.47624, -111.84587)
	if got != "4028.57N/11150.75W" {
		t.Fatalf("aprsCoord = %q", got)
	}
}

func TestMessages_Conversation(t *testing.T) {
	msgs := Messages()
	if len(msgs) < 3 {
		t.Fatalf("want >= 3 demo messages, got %d", len(msgs))
	}
	now := time.Now()
	hasIn, hasOut := false, false
	for _, m := range msgs {
		if m.OurCall != "NW5W-8" {
			t.Errorf("OurCall = %q, want NW5W-8", m.OurCall)
		}
		if m.Text == "" {
			t.Error("empty Text in message")
		}
		if len(m.Text) > 67 {
			t.Errorf("Text too long (%d > 67): %q", len(m.Text), m.Text)
		}
		if m.ThreadKind != "dm" {
			t.Errorf("ThreadKind = %q, want dm", m.ThreadKind)
		}
		if m.ThreadKey != m.PeerCall {
			t.Errorf("ThreadKey %q != PeerCall %q", m.ThreadKey, m.PeerCall)
		}
		if now.Sub(m.CreatedAt) > 30*time.Minute {
			t.Errorf("CreatedAt too old: %v", m.CreatedAt)
		}
		switch m.Direction {
		case "in":
			hasIn = true
		case "out":
			hasOut = true
		default:
			t.Errorf("unexpected Direction %q", m.Direction)
		}
	}
	if !hasIn || !hasOut {
		t.Errorf("conversation must contain both 'in' and 'out' messages; hasIn=%v hasOut=%v", hasIn, hasOut)
	}
}
