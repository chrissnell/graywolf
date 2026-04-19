package messages

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/aprs"
	"github.com/chrissnell/graywolf/pkg/ax25"
	"github.com/chrissnell/graywolf/pkg/configstore"
)

// FuzzRouterClassify drives random message bodies through the parser
// and router. Invariants:
//   - Router never panics.
//   - No auto-ACK is ever emitted for a bulletin, NWS, or self-message.
//   - Auto-ACK is never emitted with an empty msgid.
func FuzzRouterClassify(f *testing.F) {
	// Seed corpus: a mix of valid and malformed bodies.
	seeds := []struct {
		source    string
		addressee string
		text      string
		msgID     string
	}{
		{"W1ABC", "N0CALL", "hello", "001"},
		{"W1ABC", "BLNALL", "bulletin", "001"},
		{"W1ABC", "NWS-1", "weather", "007"},
		{"N0CALL", "N0CALL", "self loopback", "042"},
		{"W1ABC", "NET", "tactical", "100"},
		{"W1ABC", "N0CALL", "", ""},
		{"W1ABC", "N0CALL", "}K1XYZ>APGRWF::N0CALL   :inner", "002"},
		{"W1ABC", "N0CALL", strings.Repeat("A", 200), "999"},
		{"W1ABC", "SKY100", "nws sky", "003"},
		{"W1ABC", "CWA001", "nws cwa", "004"},
	}
	for _, s := range seeds {
		f.Add(s.source, s.addressee, s.text, s.msgID)
	}

	f.Fuzz(func(t *testing.T, source, addressee, text, msgID string) {
		if !validCallsign(source) {
			t.Skip()
		}
		if len(addressee) == 0 || len(addressee) > 9 {
			t.Skip()
		}
		// Sanitize: addressee must be printable ASCII without ':' (the
		// separator) — the wire format can't carry those, and the
		// parser already assumes well-formed headers at this point.
		for i := 0; i < len(addressee); i++ {
			c := addressee[i]
			if c < 0x20 || c > 0x7e || c == ':' {
				t.Skip()
			}
		}
		for i := 0; i < len(text); i++ {
			c := text[i]
			if c < 0x20 || c > 0x7e {
				t.Skip()
			}
		}
		if len(msgID) > 16 {
			t.Skip()
		}

		cs, err := configstore.OpenMemory()
		if err != nil {
			t.Fatalf("OpenMemory: %v", err)
		}
		defer func() { _ = cs.Close() }()
		store := NewStore(cs.DB())
		sink := &fakeTxSink{}
		ring := NewLocalTxRing(8, time.Minute)
		set := NewTacticalSet()
		set.Store(map[string]struct{}{"NET": {}, "EOC": {}})
		hub := NewEventHub(16)
		logger := slog.New(slog.NewTextHandler(io.Discard, nil))

		r, err := NewRouter(RouterConfig{
			Store:       store,
			TxSink:      sink,
			OurCall:     func() string { return "N0CALL" },
			LocalTxRing: ring,
			TacticalSet: set,
			EventHub:    hub,
			Logger:      logger,
			Clock:       &fakeClock{now: time.Unix(1_700_000_000, 0)},
		})
		if err != nil {
			t.Fatalf("NewRouter: %v", err)
		}
		r.Start(context.Background())
		defer r.Stop()

		// Build the info field.
		if len(addressee) > 9 {
			addressee = addressee[:9]
		}
		pad := addressee + strings.Repeat(" ", 9-len(addressee))
		info := ":" + pad + ":" + text
		if msgID != "" {
			info += "{" + msgID
		}
		src, err := ax25.ParseAddress(source)
		if err != nil {
			t.Skip()
		}
		dst, err := ax25.ParseAddress("APGRWF")
		if err != nil {
			t.Skip()
		}
		frame, err := ax25.NewUIFrame(src, dst, nil, []byte(info))
		if err != nil {
			t.Skip()
		}
		pkt, err := aprs.Parse(frame)
		if err != nil {
			return
		}
		pkt.Direction = aprs.DirectionRF

		if err := r.SendPacket(context.Background(), pkt); err != nil {
			t.Fatalf("SendPacket returned non-nil: %v", err)
		}

		// Give the consumer a brief window to drain. We don't assert
		// drain completion — the invariants we care about are global.
		time.Sleep(10 * time.Millisecond)

		for _, s := range sink.list() {
			if s.Frame == nil {
				t.Fatal("nil frame submitted")
			}
			info := string(s.Frame.Info)
			// Auto-ACK never has empty msgid.
			if strings.HasSuffix(info, ":ack") || strings.HasSuffix(info, ":rej") {
				t.Fatalf("auto-ACK with empty msgid: %q", info)
			}
			// Source must be our call, not the packet source (self-
			// ACK prevention).
			if s.Frame.Source.Call != "N0CALL" {
				t.Fatalf("ack source wrong: %q", s.Frame.Source.Call)
			}
			// No auto-ACK for bulletin / NWS addressees.
			if strings.HasPrefix(addressee, "BLN") ||
				strings.HasPrefix(addressee, "NWS") ||
				strings.HasPrefix(addressee, "SKY") ||
				strings.HasPrefix(addressee, "CWA") {
				t.Fatalf("auto-ACK emitted for non-ackable addressee %q", addressee)
			}
			// No auto-ACK when the source matches our base call.
			if strings.ToUpper(strings.TrimSpace(baseCall(source))) == "N0CALL" {
				t.Fatalf("auto-ACK for self-message from %q", source)
			}
		}
	})
}

// validCallsign rejects obviously-malformed callsigns before handing to
// the AX.25 parser. The parser does most of this itself; this is just
// a faster Skip path so the fuzzer spends time on interesting inputs.
func validCallsign(s string) bool {
	if len(s) == 0 || len(s) > 9 {
		return false
	}
	dash := strings.IndexByte(s, '-')
	base := s
	if dash >= 0 {
		base = s[:dash]
	}
	if len(base) == 0 || len(base) > 6 {
		return false
	}
	for i := 0; i < len(base); i++ {
		c := base[i]
		isAlnum := (c >= '0' && c <= '9') || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
		if !isAlnum {
			return false
		}
	}
	return true
}
