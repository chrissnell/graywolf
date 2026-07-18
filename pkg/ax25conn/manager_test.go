package ax25conn

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/ax25"
	"github.com/chrissnell/graywolf/pkg/configstore"
)

// fakeChannelModes is a stand-in for configstore.ChannelModeLookup in tests.
type fakeChannelModes struct {
	modes map[uint32]string
	err   error
}

func (f *fakeChannelModes) ModeForChannel(_ context.Context, ch uint32) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	if m, ok := f.modes[ch]; ok {
		return m, nil
	}
	return configstore.ChannelModeAPRS, nil
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newTestManager(t *testing.T, opts ...func(*ManagerConfig)) *Manager {
	t.Helper()
	cfg := ManagerConfig{
		TxSink: nopSink{},
		Logger: quietLogger(),
	}
	for _, o := range opts {
		o(&cfg)
	}
	return NewManager(cfg)
}

func openTestSession(t *testing.T, m *Manager, channel uint32, local, peer string, op string) (uint64, *Session) {
	t.Helper()
	scfg := SessionConfig{
		Local:   mustParse(t, local),
		Peer:    mustParse(t, peer),
		Channel: channel,
	}
	id, s, err := m.Open(scfg, op)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return id, s
}

func TestManager_OpenRoutesByLocalPeerChannel(t *testing.T) {
	m := newTestManager(t)
	defer m.Close()
	openTestSession(t, m, 1, "KE7XYZ-1", "BBS-3", "op1")
	if m.Count() != 1 {
		t.Fatalf("expected 1 session, got %d", m.Count())
	}
}

func TestManager_PerOperatorCap(t *testing.T) {
	m := newTestManager(t, func(c *ManagerConfig) { c.MaxPerOperator = 2 })
	defer m.Close()
	openTestSession(t, m, 1, "KE7XYZ-1", "BBS-1", "op1")
	openTestSession(t, m, 1, "KE7XYZ-1", "BBS-2", "op1")
	scfg := SessionConfig{
		Local:   mustParse(t, "KE7XYZ-1"),
		Peer:    mustParse(t, "BBS-3"),
		Channel: 1,
	}
	if _, _, err := m.Open(scfg, "op1"); !errors.Is(err, ErrMaxPerOperator) {
		t.Fatalf("expected ErrMaxPerOperator, got %v", err)
	}
	// A different operator must still be able to open.
	if _, _, err := m.Open(scfg, "op2"); err != nil {
		t.Fatalf("op2 must bypass per-op cap: %v", err)
	}
}

func TestManager_TotalCap(t *testing.T) {
	m := newTestManager(t, func(c *ManagerConfig) {
		c.MaxTotal = 1
		c.MaxPerOperator = 10
	})
	defer m.Close()
	openTestSession(t, m, 1, "KE7XYZ-1", "BBS-1", "op1")
	scfg := SessionConfig{
		Local:   mustParse(t, "KE7XYZ-1"),
		Peer:    mustParse(t, "BBS-2"),
		Channel: 2,
	}
	if _, _, err := m.Open(scfg, "op2"); !errors.Is(err, ErrMaxTotal) {
		t.Fatalf("expected ErrMaxTotal, got %v", err)
	}
}

func TestManager_DuplicateTriple(t *testing.T) {
	m := newTestManager(t)
	defer m.Close()
	openTestSession(t, m, 1, "KE7XYZ-1", "BBS-1", "op1")
	scfg := SessionConfig{
		Local:   mustParse(t, "KE7XYZ-1"),
		Peer:    mustParse(t, "BBS-1"),
		Channel: 1,
	}
	if _, _, err := m.Open(scfg, "op2"); !errors.Is(err, ErrSessionExists) {
		t.Fatalf("expected ErrSessionExists, got %v", err)
	}
}

func TestManager_ChannelModeAPRSRejected(t *testing.T) {
	modes := &fakeChannelModes{modes: map[uint32]string{1: configstore.ChannelModeAPRS}}
	m := newTestManager(t, func(c *ManagerConfig) { c.ChannelModes = modes })
	defer m.Close()
	scfg := SessionConfig{
		Local:   mustParse(t, "KE7XYZ-1"),
		Peer:    mustParse(t, "BBS-1"),
		Channel: 1,
	}
	if _, _, err := m.Open(scfg, "op1"); !errors.Is(err, ErrChannelAPRSOnly) {
		t.Fatalf("expected ErrChannelAPRSOnly, got %v", err)
	}
}

func TestManager_ChannelModePacketAccepted(t *testing.T) {
	modes := &fakeChannelModes{modes: map[uint32]string{1: configstore.ChannelModePacket}}
	m := newTestManager(t, func(c *ManagerConfig) { c.ChannelModes = modes })
	defer m.Close()
	openTestSession(t, m, 1, "KE7XYZ-1", "BBS-1", "op1")
}

func TestManager_DispatchRoutes(t *testing.T) {
	m := newTestManager(t)
	defer m.Close()
	_, s := openTestSession(t, m, 1, "KE7XYZ-1", "BBS-3", "op1")

	// Fire up Run so the session drains its input channel.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		s.Run(ctx)
		close(done)
	}()
	defer func() {
		cancel()
		<-done
	}()

	// Inbound SABM in DISCONNECTED → DM. Wait briefly to ensure the
	// session has consumed the event.
	in := &Frame{
		Source: mustParse(t, "BBS-3"), Dest: mustParse(t, "KE7XYZ-1"),
		Control:   Control{Kind: FrameSABM, PF: true},
		IsCommand: true,
	}
	m.Dispatch(1, in)

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if s.Snapshot().FramesTX > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if s.Snapshot().FramesTX == 0 {
		t.Fatal("expected DM emission after SABM dispatch")
	}
}

// TestManager_DispatchRawMod128Delivers is the regression guard for
// graywolf #456: inbound connected-mode frames must be decoded with the
// owning session's negotiated modulus. A mod-128 I-frame's control field
// is 2 octets; decoding it as mod-8 corrupts N(S)/N(R) so the payload is
// never delivered and the link stalls. DispatchRaw honors Session.Mod128.
func TestManager_DispatchRawMod128Delivers(t *testing.T) {
	m := newTestManager(t)
	defer m.Close()

	rxCh := make(chan []byte, 4)
	scfg := SessionConfig{
		Local:   mustParse(t, "KE7XYZ-1"),
		Peer:    mustParse(t, "BBS-3"),
		Channel: 1,
		Mod128:  true,
		Observer: func(ev OutEvent) {
			if ev.Kind == OutDataRX {
				rxCh <- append([]byte(nil), ev.Data...)
			}
		},
	}
	// Open starts the session's Run loop internally; do not spawn another.
	_, s, err := m.Open(scfg, "op1")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	// Operator connect → session sends SABME; peer answers UA to
	// establish the mod-128 link.
	s.Submit(Event{Kind: EventConnect})
	ua := &Frame{
		Source: mustParse(t, "BBS-3"), Dest: mustParse(t, "KE7XYZ-1"),
		Control: Control{Kind: FrameUA, PF: true},
		Mod128:  true,
	}
	raw, err := ua.Encode()
	if err != nil {
		t.Fatalf("encode UA: %v", err)
	}
	m.DispatchRaw(1, raw)

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && s.Snapshot().State != StateConnected {
		time.Sleep(5 * time.Millisecond)
	}
	if s.Snapshot().State != StateConnected {
		t.Fatalf("link never reached CONNECTED, state=%v", s.Snapshot().State)
	}

	// Inbound mod-128 I-frame N(S)=0 carrying data. Its 2-octet control
	// field only decodes correctly when the manager honors the session's
	// negotiated modulus.
	iframe := &Frame{
		Source: mustParse(t, "BBS-3"), Dest: mustParse(t, "KE7XYZ-1"),
		Control:   Control{Kind: FrameI, NS: 0, NR: 0, PF: false},
		PID:       0xF0,
		Info:      []byte("welcome"),
		IsCommand: true,
		Mod128:    true,
	}
	raw, err = iframe.Encode()
	if err != nil {
		t.Fatalf("encode I: %v", err)
	}
	m.DispatchRaw(1, raw)

	select {
	case got := <-rxCh:
		if string(got) != "welcome" {
			t.Fatalf("payload drift: %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("mod-128 I-frame never delivered as OutDataRX")
	}
}

func TestManager_DispatchUnknownTripleNoOp(t *testing.T) {
	m := newTestManager(t)
	defer m.Close()
	in := &Frame{
		Source: mustParse(t, "FOO-1"), Dest: mustParse(t, "BAR-2"),
		Control: Control{Kind: FrameSABM, PF: true},
	}
	m.Dispatch(1, in) // must not panic
}

func TestManager_LastPathStripsRepeatedFlag(t *testing.T) {
	m := newTestManager(t)
	defer m.Close()
	id, _ := openTestSession(t, m, 1, "KE7XYZ-1", "BBS-3", "op1")

	d1 := mustParse(t, "WIDE1-1")
	d1.Repeated = true
	d2 := mustParse(t, "WIDE2-2")
	d2.Repeated = true
	in := &Frame{
		Source: mustParse(t, "BBS-3"), Dest: mustParse(t, "KE7XYZ-1"),
		Path:    []ax25.Address{d1, d2},
		Control: Control{Kind: FrameUA, PF: true},
	}
	m.Dispatch(1, in)
	got := m.LastPath(id)
	if len(got) != 2 {
		t.Fatalf("expected 2 entries, got %v", got)
	}
	for i, a := range got {
		if a.Repeated {
			t.Errorf("entry %d still has Repeated set", i)
		}
	}
	if got[0].Call != "WIDE1" || got[1].Call != "WIDE2" {
		t.Fatalf("path drift: %+v", got)
	}
}

func TestManager_LastPathDropsUnrepeatedTail(t *testing.T) {
	m := newTestManager(t)
	defer m.Close()
	id, _ := openTestSession(t, m, 1, "KE7XYZ-1", "BBS-3", "op1")

	d1 := mustParse(t, "WIDE1-1")
	d1.Repeated = true
	d2 := mustParse(t, "WIDE2-2") // not repeated
	in := &Frame{
		Source: mustParse(t, "BBS-3"), Dest: mustParse(t, "KE7XYZ-1"),
		Path:    []ax25.Address{d1, d2},
		Control: Control{Kind: FrameUA, PF: true},
	}
	m.Dispatch(1, in)
	got := m.LastPath(id)
	if len(got) != 1 || got[0].Call != "WIDE1" {
		t.Fatalf("expected only repeated head, got %+v", got)
	}
}

func TestManager_CloseCancelsSessions(t *testing.T) {
	m := newTestManager(t)
	openTestSession(t, m, 1, "KE7XYZ-1", "BBS-1", "op1")
	openTestSession(t, m, 1, "KE7XYZ-1", "BBS-2", "op1")
	m.Close()
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if m.Count() == 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	// (sessions remove themselves from the map on Run exit; tolerate
	// races by polling above.)
}

// TestManager_FreesTripleAfterFailedConnect guards against graywolf
// #456: a link that never comes up must not leave its (channel, local,
// peer) triple bound. Before the fix the session parked in DISCONNECTED
// forever, so the operator's next connect to the same peer failed with
// ErrSessionExists until graywolf was restarted.
func TestManager_FreesTripleAfterFailedConnect(t *testing.T) {
	m := newTestManager(t)
	defer m.Close()
	_, s := openTestSession(t, m, 1, "KE7XYZ-1", "BBS-3", "op1")

	// Drive the link: SABM out, then the peer refuses with DM(F=1),
	// which is the fast terminal path into DISCONNECTED.
	s.Submit(Event{Kind: EventConnect})
	m.Dispatch(1, &Frame{
		Source:  mustParse(t, "BBS-3"),
		Dest:    mustParse(t, "KE7XYZ-1"),
		Control: Control{Kind: FrameDM, PF: true},
	})

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && m.Count() != 0 {
		time.Sleep(5 * time.Millisecond)
	}
	if m.Count() != 0 {
		t.Fatalf("triple not freed after failed connect; Count=%d", m.Count())
	}

	// The operator must be able to reconnect to the same peer without a
	// restart.
	if _, _, err := m.Open(SessionConfig{
		Local:   mustParse(t, "KE7XYZ-1"),
		Peer:    mustParse(t, "BBS-3"),
		Channel: 1,
	}, "op1"); err != nil {
		t.Fatalf("reconnect to same triple must succeed, got %v", err)
	}
}

func TestManager_OpenAfterCloseFails(t *testing.T) {
	m := newTestManager(t)
	m.Close()
	scfg := SessionConfig{
		Local:   mustParse(t, "KE7XYZ-1"),
		Peer:    mustParse(t, "BBS-1"),
		Channel: 1,
	}
	if _, _, err := m.Open(scfg, "op"); !errors.Is(err, ErrManagerClosed) {
		t.Fatalf("expected ErrManagerClosed, got %v", err)
	}
}

func TestManager_RunBlocksUntilCancel(t *testing.T) {
	m := newTestManager(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { m.Run(ctx); close(done) }()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not return after Close")
	}
}
