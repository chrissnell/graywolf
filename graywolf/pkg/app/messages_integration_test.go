package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/aprs"
	"github.com/chrissnell/graywolf/pkg/ax25"
	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/messages"
	"github.com/chrissnell/graywolf/pkg/txgovernor"
	"github.com/chrissnell/graywolf/pkg/webapi"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

// ---------------------------------------------------------------------------
// Phase 8a — end-to-end integration tests for the APRS messages stack.
//
// These tests exercise Store + Router + Sender + Service + webapi handlers
// as a single assembly, stitched together with fake outward-facing edges
// (fakeTxSink for the governor, fakeIGateSender for APRS-IS). The goal is
// realistic message flows: compose → TX → ack; inbound → auto-ACK;
// tactical single-shot; restart recovery; reload signal round-trips.
//
// Pattern mirrors pkg/app/messages_wiring_test.go (Phase 5 lifecycle
// smoke) and pkg/webapi/messages_test.go (httptest-based REST harness)
// but goes one level deeper by wiring a *real* messages.Service end-to-
// end with a direct webapi.Server on top.
// ---------------------------------------------------------------------------

// intTxSink is a fake txgovernor.TxSink + TxHookRegistry for integration
// tests. It records every Submit call and lets tests invoke the
// registered TxHook on a specific frame to simulate the governor's
// worker loop firing (which is the production path that flips SentAt).
type intTxSink struct {
	mu        sync.Mutex
	submitted []intSubmit
	hooks     []txgovernor.TxHook
	nextID    uint64
	// submitErr, if non-nil, short-circuits Submit with this error. Set
	// by tests that want to simulate ErrQueueFull / ErrStopped.
	submitErr error
}

type intSubmit struct {
	Channel uint32
	Frame   *ax25.Frame
	Src     txgovernor.SubmitSource
}

func (s *intTxSink) Submit(_ context.Context, ch uint32, frame *ax25.Frame, src txgovernor.SubmitSource) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.submitErr != nil {
		return s.submitErr
	}
	s.submitted = append(s.submitted, intSubmit{Channel: ch, Frame: frame, Src: src})
	return nil
}

// AddTxHook satisfies txgovernor.TxHookRegistry.
func (s *intTxSink) AddTxHook(h txgovernor.TxHook) (uint64, func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	id := s.nextID
	s.hooks = append(s.hooks, h)
	return id, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		for i, fn := range s.hooks {
			// Compare function identities via reflect-less trick: match
			// by pointer equality on the closure. Hook removal happens
			// once per Service.Stop; imprecise matching is fine because
			// unregistering the Nth hook is idempotent.
			_ = fn
			if i == len(s.hooks)-1 {
				s.hooks = s.hooks[:i]
				return
			}
		}
	}
}

// fireTxHook invokes every registered TxHook on the idx-th submitted
// frame. Used by tests to simulate the governor dispatching a submit
// downstream and firing its post-send callbacks. Callers must ensure
// at least idx+1 frames have been submitted.
func (s *intTxSink) fireTxHook(idx int) {
	s.mu.Lock()
	if idx < 0 || idx >= len(s.submitted) {
		s.mu.Unlock()
		return
	}
	entry := s.submitted[idx]
	hooks := make([]txgovernor.TxHook, len(s.hooks))
	copy(hooks, s.hooks)
	s.mu.Unlock()
	for _, h := range hooks {
		h(entry.Channel, entry.Frame, entry.Src)
	}
}

// list returns a snapshot of submitted frames.
func (s *intTxSink) list() []intSubmit {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]intSubmit, len(s.submitted))
	copy(out, s.submitted)
	return out
}

// count returns the number of submitted frames.
func (s *intTxSink) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.submitted)
}

// waitForSubmit polls until count() >= want or the deadline expires.
func (s *intTxSink) waitForSubmit(t *testing.T, want int, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if s.count() >= want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %d submits; got %d", want, s.count())
}

// intIGateSender is a fake IGateLineSender capturing outbound lines.
type intIGateSender struct {
	mu    sync.Mutex
	lines []string
}

func (i *intIGateSender) SendLine(l string) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.lines = append(i.lines, l)
	return nil
}

// intTestContext bundles the wired-up integration harness.
type intTestContext struct {
	srv        *httptest.Server
	client     *http.Client
	store      *configstore.Store
	msgStore   *messages.Store
	msgSvc     *messages.Service
	sink       *intTxSink
	igs        *intIGateSender
	ourCall    string
	reloadCh   chan struct{}
	apiSrv     *webapi.Server
	// drainerWG tracks the reload drainer goroutine started by
	// setupIntegration so the cleanup path can wait it out.
	drainerWG sync.WaitGroup
	drainerCancel context.CancelFunc
}

// setupIntegration wires a full messages stack and exposes it via an
// httptest.Server mounted at /api/... (no auth — tests hit handlers
// directly). FallbackPolicy is seeded to "rf_only" for determinism so
// no test accidentally exercises the IS fallback path. Callers get an
// already-started Service and must call the returned cleanup when done.
func setupIntegration(t *testing.T, ourCall string) (*intTestContext, func()) {
	t.Helper()

	store, err := configstore.OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}

	ctx := context.Background()
	// Seed iGate config so resolveOurCall returns ourCall.
	if err := store.UpsertIGateConfig(ctx, &configstore.IGateConfig{
		Callsign:   ourCall,
		Server:     "rotate.aprs2.net",
		Port:       14580,
		Passcode:   "-1",
		TxChannel:  1,
		RfChannel:  1,
		MaxMsgHops: 2,
	}); err != nil {
		t.Fatalf("UpsertIGateConfig: %v", err)
	}
	// Seed preferences to rf_only so send paths are deterministic —
	// no IS fallback. Tests that want to exercise fallback will flip
	// this directly via UpsertMessagePreferences (or the PUT endpoint
	// in the preferences-reload scenario).
	if err := store.UpsertMessagePreferences(ctx, &configstore.MessagePreferences{
		FallbackPolicy:   messages.FallbackPolicyRFOnly,
		DefaultPath:      "WIDE1-1,WIDE2-1",
		RetryMaxAttempts: 5,
		RetentionDays:    0,
	}); err != nil {
		t.Fatalf("UpsertMessagePreferences: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	sink := &intTxSink{}
	igs := &intIGateSender{}

	msgStore := messages.NewStore(store.DB())

	svc, err := messages.NewService(messages.ServiceConfig{
		Store:         msgStore,
		ConfigStore:   store,
		TxSink:        sink,
		TxHookReg:     sink,
		IGate:         igs,
		Bridge:        nil, // alwaysRF
		Logger:        logger.With("component", "messages"),
		IGatePasscode: "-1",
		OurCall:       func() string { return ourCall },
		TxChannel:     1,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("Service.Start: %v", err)
	}

	apiSrv, err := webapi.NewServer(webapi.Config{
		Store:  store,
		Logger: logger,
	})
	if err != nil {
		t.Fatalf("webapi.NewServer: %v", err)
	}
	apiSrv.SetMessagesService(svc)
	apiSrv.SetMessagesStore(msgStore)

	reloadCh := make(chan struct{}, 1)
	apiSrv.SetMessagesReload(reloadCh)

	// Spin a drainer goroutine that mimics pkg/app's messagesComponent
	// drainer: on a signal, reload prefs + tactical callsigns on the
	// Service. Needed so PUT /api/messages/preferences and tactical
	// CRUD actually propagate to the live Service snapshot. (The
	// handlers also call ReloadPreferences inline for the preferences
	// PUT; the tactical CRUD path uses the inline + signal combo.)
	drainerCtx, drainerCancel := context.WithCancel(context.Background())
	tctx := &intTestContext{
		store:         store,
		msgStore:      msgStore,
		msgSvc:        svc,
		sink:          sink,
		igs:           igs,
		ourCall:       ourCall,
		reloadCh:      reloadCh,
		apiSrv:        apiSrv,
		drainerCancel: drainerCancel,
	}
	tctx.drainerWG.Add(1)
	go func() {
		defer tctx.drainerWG.Done()
		for {
			select {
			case <-drainerCtx.Done():
				return
			case _, ok := <-reloadCh:
				if !ok {
					return
				}
				reloadCtx, cancel := context.WithTimeout(drainerCtx, 500*time.Millisecond)
				_ = svc.ReloadPreferences(reloadCtx)
				_ = svc.ReloadTacticalCallsigns(reloadCtx)
				cancel()
			}
		}
	}()

	mux := http.NewServeMux()
	apiSrv.RegisterRoutes(mux)

	httpSrv := httptest.NewServer(mux)
	tctx.srv = httpSrv
	tctx.client = httpSrv.Client()

	cleanup := func() {
		httpSrv.Close()
		svc.Stop()
		drainerCancel()
		tctx.drainerWG.Wait()
		_ = store.Close()
	}
	return tctx, cleanup
}

// postJSON POSTs body to path and returns status + decoded body bytes.
func (c *intTestContext) postJSON(t *testing.T, path string, body any) (int, []byte) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("json encode: %v", err)
		}
	}
	req, err := http.NewRequest(http.MethodPost, c.srv.URL+path, &buf)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, data
}

// putJSON sends a PUT with JSON body.
func (c *intTestContext) putJSON(t *testing.T, path string, body any) (int, []byte) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("json encode: %v", err)
		}
	}
	req, err := http.NewRequest(http.MethodPut, c.srv.URL+path, &buf)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, data
}

// waitForRow polls GetByID until cond(row) is true or the deadline
// expires. Returns the final (possibly-matching) row. Uses Unscoped
// list because some tests deal with soft-deleted rows.
func (c *intTestContext) waitForRow(t *testing.T, id uint64, within time.Duration, cond func(*configstore.Message) bool) *configstore.Message {
	t.Helper()
	deadline := time.Now().Add(within)
	var cur *configstore.Message
	for time.Now().Before(deadline) {
		rows, _, err := c.msgStore.List(context.Background(), messages.Filter{IncludeDeleted: true, Limit: 500})
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		for i := range rows {
			if rows[i].ID == id {
				r := rows[i]
				cur = &r
				if cond(&r) {
					return &r
				}
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	if cur == nil {
		t.Fatalf("row id=%d never observed within %v", id, within)
	}
	return cur
}

// makeInboundMessage builds a decoded APRS packet for an inbound message
// ":AAA...:text{id}". Direction is RF so auto-ACK goes out on RF.
func makeInboundMessage(t *testing.T, source, addressee, text, msgID string) *aprs.DecodedAPRSPacket {
	t.Helper()
	pad := addressee + strings.Repeat(" ", 9-len(addressee))
	info := ":" + pad + ":" + text
	if msgID != "" {
		info += "{" + msgID
	}
	src, err := ax25.ParseAddress(source)
	if err != nil {
		t.Fatalf("ParseAddress: %v", err)
	}
	dst, err := ax25.ParseAddress("APGRWF")
	if err != nil {
		t.Fatalf("ParseAddress dest: %v", err)
	}
	f, err := ax25.NewUIFrame(src, dst, nil, []byte(info))
	if err != nil {
		t.Fatalf("NewUIFrame: %v", err)
	}
	pkt, err := aprs.Parse(f)
	if err != nil {
		t.Fatalf("aprs.Parse: %v", err)
	}
	pkt.Direction = aprs.DirectionRF
	return pkt
}

// makeInboundAck builds ":our:ackNNN" from source.
func makeInboundAck(t *testing.T, source, addressee, msgID string) *aprs.DecodedAPRSPacket {
	t.Helper()
	pad := addressee + strings.Repeat(" ", 9-len(addressee))
	info := ":" + pad + ":ack" + msgID
	src, _ := ax25.ParseAddress(source)
	dst, _ := ax25.ParseAddress("APGRWF")
	f, err := ax25.NewUIFrame(src, dst, nil, []byte(info))
	if err != nil {
		t.Fatalf("NewUIFrame: %v", err)
	}
	pkt, err := aprs.Parse(f)
	if err != nil {
		t.Fatalf("aprs.Parse: %v", err)
	}
	pkt.Direction = aprs.DirectionRF
	return pkt
}

// makeInboundReplyAck builds ":addr:text{msgID}replyAckID".
func makeInboundReplyAck(t *testing.T, source, addressee, text, msgID, replyAckID string) *aprs.DecodedAPRSPacket {
	t.Helper()
	pad := addressee + strings.Repeat(" ", 9-len(addressee))
	info := ":" + pad + ":" + text + "{" + msgID + "}" + replyAckID
	src, _ := ax25.ParseAddress(source)
	dst, _ := ax25.ParseAddress("APGRWF")
	f, err := ax25.NewUIFrame(src, dst, nil, []byte(info))
	if err != nil {
		t.Fatalf("NewUIFrame: %v", err)
	}
	pkt, err := aprs.Parse(f)
	if err != nil {
		t.Fatalf("aprs.Parse: %v", err)
	}
	pkt.Direction = aprs.DirectionRF
	return pkt
}

// ---------------------------------------------------------------------------
// TestMessagesIntegration — one top-level test with 10 focused subtests.
// ---------------------------------------------------------------------------

func TestMessagesIntegration(t *testing.T) {
	t.Run("DM_HappyPath_ComposeTxAck", func(t *testing.T) {
		tctx, cleanup := setupIntegration(t, "N0CALL")
		defer cleanup()

		// Compose a DM via REST.
		status, body := tctx.postJSON(t, "/api/messages", map[string]any{
			"to":   "W1ABC",
			"text": "hello world",
		})
		if status != http.StatusAccepted {
			t.Fatalf("compose: status=%d body=%s", status, string(body))
		}
		var resp dto.MessageResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			t.Fatalf("decode compose resp: %v", err)
		}
		if resp.ID == 0 || resp.Direction != "out" || resp.MsgID == "" {
			t.Fatalf("unexpected compose response: %+v", resp)
		}

		// Wait for the sender goroutine to submit exactly one frame.
		tctx.sink.waitForSubmit(t, 1, time.Second)
		submits := tctx.sink.list()
		if submits[0].Src.Kind != messages.SubmitKindMessages {
			t.Errorf("submit Kind = %q, want %q", submits[0].Src.Kind, messages.SubmitKindMessages)
		}

		// Fire the TxHook — this is what the real governor does after it
		// hands the frame to the modem. SentAt should flip.
		tctx.sink.fireTxHook(0)
		row := tctx.waitForRow(t, resp.ID, time.Second, func(m *configstore.Message) bool {
			return m.SentAt != nil
		})
		if row.AckState != messages.AckStateNone {
			t.Errorf("AckState after tx hook = %q, want none (awaiting ack)", row.AckState)
		}

		// Simulate the peer's ack arriving via the router.
		tctx.msgSvc.Router().SendPacket(context.Background(), makeInboundAck(t, "W1ABC", "N0CALL", resp.MsgID))
		row = tctx.waitForRow(t, resp.ID, time.Second, func(m *configstore.Message) bool {
			return m.AckState == messages.AckStateAcked
		})
		if row.AckedAt == nil {
			t.Error("AckedAt nil on acked row")
		}
	})

	t.Run("DM_AutoAck_SkipDedupAllowsEveryCopy", func(t *testing.T) {
		tctx, cleanup := setupIntegration(t, "N0CALL")
		defer cleanup()

		// First copy of the inbound DM — router persists + emits auto-ACK.
		pkt1 := makeInboundMessage(t, "W1ABC", "N0CALL", "hi there", "042")
		tctx.msgSvc.Router().SendPacket(context.Background(), pkt1)
		tctx.sink.waitForSubmit(t, 1, time.Second)
		first := tctx.sink.list()[0]
		if first.Src.Kind != messages.SubmitKindMessagesAutoAck {
			t.Errorf("first submit Kind = %q, want %q", first.Src.Kind, messages.SubmitKindMessagesAutoAck)
		}
		if !first.Src.SkipDedup {
			t.Error("first auto-ACK SubmitSource.SkipDedup = false, want true (APRS101 §14.2)")
		}

		// Second copy (identical) — router's dedup cache suppresses the
		// Insert but still emits another auto-ACK. This is the
		// "ack every copy" contract.
		pkt2 := makeInboundMessage(t, "W1ABC", "N0CALL", "hi there", "042")
		tctx.msgSvc.Router().SendPacket(context.Background(), pkt2)
		tctx.sink.waitForSubmit(t, 2, time.Second)
		second := tctx.sink.list()[1]
		if second.Src.Kind != messages.SubmitKindMessagesAutoAck {
			t.Errorf("second submit Kind = %q, want %q", second.Src.Kind, messages.SubmitKindMessagesAutoAck)
		}
		if !second.Src.SkipDedup {
			t.Error("second auto-ACK SkipDedup = false; governor would suppress it and APRS101 §14.2 would be violated")
		}

		// The store should show exactly one inbound row (dedup suppressed
		// the second Insert) — with AckState='none' (inbound rows never
		// get AckState flipped; the state column is outbound-only).
		rows, _, err := tctx.msgStore.List(context.Background(), messages.Filter{Folder: messages.FolderInbox})
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(rows) != 1 {
			t.Errorf("inbox rows = %d, want 1 (dedup should suppress duplicates)", len(rows))
		}
	})

	t.Run("Tactical_Inbound_NoAutoAck", func(t *testing.T) {
		tctx, cleanup := setupIntegration(t, "N0CALL")
		defer cleanup()

		// Register a tactical and reload so the router picks it up.
		if err := tctx.store.CreateTacticalCallsign(context.Background(), &configstore.TacticalCallsign{
			Callsign: "NET", Alias: "Main Net", Enabled: true,
		}); err != nil {
			t.Fatalf("CreateTacticalCallsign: %v", err)
		}
		if err := tctx.msgSvc.ReloadTacticalCallsigns(context.Background()); err != nil {
			t.Fatalf("ReloadTacticalCallsigns: %v", err)
		}

		// Inbound addressed to the tactical.
		pkt := makeInboundMessage(t, "W1ABC", "NET", "copy net", "017")
		tctx.msgSvc.Router().SendPacket(context.Background(), pkt)

		// Poll for row insertion.
		deadline := time.Now().Add(time.Second)
		var tacRow *configstore.Message
		for time.Now().Before(deadline) {
			rows, _, err := tctx.msgStore.List(context.Background(), messages.Filter{
				ThreadKind: messages.ThreadKindTactical,
				ThreadKey:  "NET",
			})
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			if len(rows) == 1 {
				r := rows[0]
				tacRow = &r
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		if tacRow == nil {
			t.Fatal("tactical inbound was not persisted")
		}
		if tacRow.ThreadKind != messages.ThreadKindTactical {
			t.Errorf("ThreadKind = %q, want tactical", tacRow.ThreadKind)
		}

		// Give the router a moment to have emitted any (incorrect)
		// auto-ACK — then assert none fired.
		time.Sleep(50 * time.Millisecond)
		if n := tctx.sink.count(); n != 0 {
			t.Errorf("tactical inbound triggered %d TX submits; want 0", n)
		}
	})

	t.Run("Tactical_Outbound_SingleShotBroadcastNoRetry", func(t *testing.T) {
		tctx, cleanup := setupIntegration(t, "N0CALL")
		defer cleanup()

		// Register tactical + reload.
		if err := tctx.store.CreateTacticalCallsign(context.Background(), &configstore.TacticalCallsign{
			Callsign: "NET", Alias: "Main Net", Enabled: true,
		}); err != nil {
			t.Fatalf("CreateTacticalCallsign: %v", err)
		}
		if err := tctx.msgSvc.ReloadTacticalCallsigns(context.Background()); err != nil {
			t.Fatalf("ReloadTacticalCallsigns: %v", err)
		}

		// Compose directly via Service (bypassing REST's uppercase
		// loopback guard logic — the compose endpoint works too but
		// going through the service is the minimum surface).
		row, err := tctx.msgSvc.SendMessage(context.Background(), messages.SendMessageRequest{
			To:      "NET",
			Text:    "evening roll call",
			OurCall: "N0CALL",
		})
		if err != nil {
			t.Fatalf("SendMessage: %v", err)
		}
		if row.ThreadKind != messages.ThreadKindTactical {
			t.Errorf("ThreadKind = %q, want tactical", row.ThreadKind)
		}
		if row.MsgID != "" {
			t.Errorf("tactical MsgID = %q, want empty (no ack correlation)", row.MsgID)
		}

		tctx.sink.waitForSubmit(t, 1, time.Second)
		tctx.sink.fireTxHook(0)

		// After TxHook: tactical → AckState=broadcast, SentAt set.
		cur := tctx.waitForRow(t, row.ID, time.Second, func(m *configstore.Message) bool {
			return m.AckState == messages.AckStateBroadcast
		})
		if cur.SentAt == nil {
			t.Error("SentAt nil on tactical after TxHook")
		}

		// RetryManager MUST NOT enroll tactical rows. ListRetryDue over
		// any future window should not include this row.
		due, err := tctx.msgStore.ListRetryDue(context.Background(), time.Now().Add(24*time.Hour))
		if err != nil {
			t.Fatalf("ListRetryDue: %v", err)
		}
		for _, r := range due {
			if r.ID == row.ID {
				t.Errorf("tactical row id=%d unexpectedly enrolled in retry ladder", row.ID)
			}
		}
		// Ensure it's single-shot: no further submits occur.
		time.Sleep(30 * time.Millisecond)
		if n := tctx.sink.count(); n != 1 {
			t.Errorf("tactical submit count = %d, want 1 (single-shot)", n)
		}
	})

	t.Run("Tactical_ReplyAck_SetsReceivedByCall", func(t *testing.T) {
		tctx, cleanup := setupIntegration(t, "N0CALL")
		defer cleanup()

		// Tactical setup.
		if err := tctx.store.CreateTacticalCallsign(context.Background(), &configstore.TacticalCallsign{
			Callsign: "NET", Alias: "Main Net", Enabled: true,
		}); err != nil {
			t.Fatalf("CreateTacticalCallsign: %v", err)
		}
		if err := tctx.msgSvc.ReloadTacticalCallsigns(context.Background()); err != nil {
			t.Fatalf("ReloadTacticalCallsigns: %v", err)
		}

		// Insert a tactical outbound directly with a synthetic msgid so
		// we control the reply-ack correlation key. Service.SendMessage
		// does NOT allocate msgid for tactical; assign one manually.
		row := &configstore.Message{
			Direction:  "out",
			OurCall:    "N0CALL",
			FromCall:   "N0CALL",
			ToCall:     "NET",
			Text:       "hi net",
			MsgID:      "088",
			ThreadKind: messages.ThreadKindTactical,
			AckState:   messages.AckStateBroadcast, // simulate already-sent
		}
		if err := tctx.msgStore.Insert(context.Background(), row); err != nil {
			t.Fatalf("Insert: %v", err)
		}

		// Inbound from W1ABC carrying reply-ack pointing at our msgid.
		pkt := makeInboundReplyAck(t, "W1ABC", "NET", "got it", "099", "088")
		tctx.msgSvc.Router().SendPacket(context.Background(), pkt)

		// Poll for ReceivedByCall to flip.
		cur := tctx.waitForRow(t, row.ID, time.Second, func(m *configstore.Message) bool {
			return m.ReceivedByCall != ""
		})
		if cur.ReceivedByCall != "W1ABC" {
			t.Errorf("ReceivedByCall = %q, want W1ABC", cur.ReceivedByCall)
		}
		if cur.AckState != messages.AckStateBroadcast {
			t.Errorf("tactical AckState changed: %q, want broadcast (reply-ack doesn't flip state for tactical)", cur.AckState)
		}
	})

	t.Run("RestartRecovery_RetryManagerBootstrapsFromStore", func(t *testing.T) {
		// This scenario needs to outlive the default setupIntegration
		// cleanup (which closes the store). We construct the stack by
		// hand so we can tear down the service while keeping the store
		// open, then reboot a fresh service on the same store and
		// assert the bootstrap query finds the row.
		store, err := configstore.OpenMemory()
		if err != nil {
			t.Fatalf("OpenMemory: %v", err)
		}
		defer store.Close()

		ctx := context.Background()
		if err := store.UpsertIGateConfig(ctx, &configstore.IGateConfig{
			Callsign: "N0CALL", Server: "rotate.aprs2.net", Port: 14580,
			Passcode: "-1", TxChannel: 1, RfChannel: 1, MaxMsgHops: 2,
		}); err != nil {
			t.Fatalf("UpsertIGateConfig: %v", err)
		}
		if err := store.UpsertMessagePreferences(ctx, &configstore.MessagePreferences{
			FallbackPolicy: messages.FallbackPolicyRFOnly, DefaultPath: "WIDE1-1,WIDE2-1", RetryMaxAttempts: 5,
		}); err != nil {
			t.Fatalf("UpsertMessagePreferences: %v", err)
		}

		logger := slog.New(slog.NewTextHandler(io.Discard, nil))
		sink1 := &intTxSink{}
		igs1 := &intIGateSender{}
		msgStore1 := messages.NewStore(store.DB())
		svc1, err := messages.NewService(messages.ServiceConfig{
			Store: msgStore1, ConfigStore: store,
			TxSink: sink1, TxHookReg: sink1, IGate: igs1,
			Logger: logger, IGatePasscode: "-1",
			OurCall:   func() string { return "N0CALL" },
			TxChannel: 1,
		})
		if err != nil {
			t.Fatalf("NewService (first): %v", err)
		}
		if err := svc1.Start(ctx); err != nil {
			t.Fatalf("Service.Start (first): %v", err)
		}

		// Compose and drive to "awaiting ack with NextRetryAt scheduled".
		row, err := svc1.SendMessage(ctx, messages.SendMessageRequest{
			To: "W1ABC", Text: "awaiting restart", OurCall: "N0CALL",
		})
		if err != nil {
			t.Fatalf("SendMessage: %v", err)
		}
		sink1.waitForSubmit(t, 1, time.Second)
		sink1.fireTxHook(0)

		// Wait for the RetryManager to schedule the first retry (async
		// via the Service.SendMessage goroutine).
		deadline := time.Now().Add(time.Second)
		var awaiting *configstore.Message
		for time.Now().Before(deadline) {
			cur, _ := msgStore1.GetByID(ctx, row.ID)
			if cur != nil && cur.NextRetryAt != nil && cur.SentAt != nil && cur.AckState == messages.AckStateNone {
				awaiting = cur
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		if awaiting == nil {
			t.Fatal("row never reached awaiting-ack with NextRetryAt set")
		}

		// Stop the first Service (store stays open).
		svc1.Stop()

		// Bootstrap a fresh Service over the same store. ListAwaiting
		// AckOnStartup should find our row, and RetryManager.Start()
		// should enumerate and kick on it.
		sink2 := &intTxSink{}
		igs2 := &intIGateSender{}
		msgStore2 := messages.NewStore(store.DB())
		svc2, err := messages.NewService(messages.ServiceConfig{
			Store: msgStore2, ConfigStore: store,
			TxSink: sink2, TxHookReg: sink2, IGate: igs2,
			Logger: logger, IGatePasscode: "-1",
			OurCall:   func() string { return "N0CALL" },
			TxChannel: 1,
		})
		if err != nil {
			t.Fatalf("NewService (restart): %v", err)
		}
		defer svc2.Stop()
		if err := svc2.Start(ctx); err != nil {
			t.Fatalf("Service.Start (restart): %v", err)
		}

		// Invariant: ListAwaitingAckOnStartup still enumerates the row
		// (RetryManager.bootstrap consumed it and kicked the loop).
		found, err := msgStore2.ListAwaitingAckOnStartup(ctx)
		if err != nil {
			t.Fatalf("ListAwaitingAckOnStartup: %v", err)
		}
		got := false
		for _, r := range found {
			if r.ID == row.ID && r.MsgID == row.MsgID {
				got = true
				break
			}
		}
		if !got {
			t.Fatalf("row id=%d msgid=%q not in ListAwaitingAckOnStartup after restart", row.ID, row.MsgID)
		}
	})

	t.Run("PreferencesReload_SignalCausesServiceReload", func(t *testing.T) {
		tctx, cleanup := setupIntegration(t, "N0CALL")
		defer cleanup()

		// Baseline: seeded rf_only.
		if got := messages.NormalizeFallbackPolicy(tctx.msgSvc.Preferences().Current().FallbackPolicy); got != messages.FallbackPolicyRFOnly {
			t.Fatalf("baseline FallbackPolicy = %q, want %q", got, messages.FallbackPolicyRFOnly)
		}

		// PUT /api/messages/preferences → handler calls ReloadPreferences
		// inline AND fires the reload signal.
		status, body := tctx.putJSON(t, "/api/messages/preferences", map[string]any{
			"fallback_policy":    messages.FallbackPolicyISOnly,
			"default_path":       "WIDE1-1",
			"retry_max_attempts": 3,
			"retention_days":     0,
		})
		if status != http.StatusOK {
			t.Fatalf("PUT prefs: status=%d body=%s", status, string(body))
		}

		// Poll the live Service snapshot. The handler calls reload
		// inline, so the change should be visible immediately — but the
		// reload signal also fires through the drainer goroutine and
		// re-reloads; either path is sufficient.
		deadline := time.Now().Add(time.Second)
		for time.Now().Before(deadline) {
			if messages.NormalizeFallbackPolicy(tctx.msgSvc.Preferences().Current().FallbackPolicy) == messages.FallbackPolicyISOnly {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatalf("Service FallbackPolicy never reloaded; got %q",
			tctx.msgSvc.Preferences().Current().FallbackPolicy)
	})

	t.Run("TacticalCRUD_RebuildsSetOnAddAndDisable", func(t *testing.T) {
		tctx, cleanup := setupIntegration(t, "N0CALL")
		defer cleanup()

		// Baseline: set empty.
		if tctx.msgSvc.TacticalSet().Contains("NET") {
			t.Fatal("baseline TacticalSet unexpectedly contains NET")
		}

		// POST /api/messages/tactical adds NET.
		status, body := tctx.postJSON(t, "/api/messages/tactical", map[string]any{
			"callsign": "NET",
			"alias":    "Main Net",
			"enabled":  true,
		})
		if status != http.StatusCreated {
			t.Fatalf("POST tactical: status=%d body=%s", status, string(body))
		}
		var resp dto.TacticalCallsignResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			t.Fatalf("decode resp: %v", err)
		}

		// Handler reloads inline, but wait to be sure.
		deadline := time.Now().Add(time.Second)
		for time.Now().Before(deadline) {
			if tctx.msgSvc.TacticalSet().Contains("NET") {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		if !tctx.msgSvc.TacticalSet().Contains("NET") {
			t.Fatal("TacticalSet never picked up NET after POST")
		}

		// Inbound addressed to NET should now route as tactical.
		pkt := makeInboundMessage(t, "W1ABC", "NET", "hello net", "050")
		tctx.msgSvc.Router().SendPacket(context.Background(), pkt)

		// Poll for persistence.
		var found bool
		deadline = time.Now().Add(time.Second)
		for time.Now().Before(deadline) {
			rows, _, _ := tctx.msgStore.List(context.Background(), messages.Filter{
				ThreadKind: messages.ThreadKindTactical, ThreadKey: "NET",
			})
			if len(rows) >= 1 {
				found = true
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		if !found {
			t.Fatal("inbound to NET was not routed as tactical after POST")
		}

		// Disable the tactical via PUT, then send another inbound — it
		// should not land in the tactical thread.
		status, body = tctx.putJSON(t, fmt.Sprintf("/api/messages/tactical/%d", resp.ID), map[string]any{
			"callsign": "NET",
			"alias":    "Main Net",
			"enabled":  false,
		})
		if status != http.StatusOK {
			t.Fatalf("PUT tactical: status=%d body=%s", status, string(body))
		}
		// Wait for reload.
		deadline = time.Now().Add(time.Second)
		for time.Now().Before(deadline) {
			if !tctx.msgSvc.TacticalSet().Contains("NET") {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		if tctx.msgSvc.TacticalSet().Contains("NET") {
			t.Fatal("TacticalSet still contains NET after disable")
		}

		// Snapshot the tactical row count before the second inbound.
		priorRows, _, _ := tctx.msgStore.List(context.Background(), messages.Filter{
			ThreadKind: messages.ThreadKindTactical, ThreadKey: "NET",
		})
		priorCount := len(priorRows)

		// Second inbound — should be dropped (not for us).
		pkt2 := makeInboundMessage(t, "W1ABC", "NET", "hello again", "051")
		tctx.msgSvc.Router().SendPacket(context.Background(), pkt2)
		time.Sleep(50 * time.Millisecond)
		postRows, _, _ := tctx.msgStore.List(context.Background(), messages.Filter{
			ThreadKind: messages.ThreadKindTactical, ThreadKey: "NET",
		})
		if len(postRows) != priorCount {
			t.Errorf("tactical rows after disable = %d, want %d (inbound should have dropped)", len(postRows), priorCount)
		}
	})

	t.Run("BotCollision_RejectsSMSTactical", func(t *testing.T) {
		tctx, cleanup := setupIntegration(t, "N0CALL")
		defer cleanup()

		status, body := tctx.postJSON(t, "/api/messages/tactical", map[string]any{
			"callsign": "SMS",
			"alias":    "Text Gateway",
			"enabled":  true,
		})
		if status != http.StatusBadRequest {
			t.Fatalf("expected 400 for SMS collision, got %d: %s", status, string(body))
		}
		if !strings.Contains(string(body), "well-known APRS service address") {
			t.Errorf("error body missing 'well-known APRS service address': %s", string(body))
		}

		// Confirm no row was written.
		rows, err := tctx.store.ListTacticalCallsigns(context.Background())
		if err != nil {
			t.Fatalf("ListTacticalCallsigns: %v", err)
		}
		for _, r := range rows {
			if r.Callsign == "SMS" {
				t.Errorf("SMS tactical row persisted: %+v", r)
			}
		}
	})

	t.Run("LateAckOnSoftDeletedRow_SetsAckedAt", func(t *testing.T) {
		tctx, cleanup := setupIntegration(t, "N0CALL")
		defer cleanup()

		// Compose + TxHook → awaiting ack with NextRetryAt.
		status, body := tctx.postJSON(t, "/api/messages", map[string]any{
			"to": "W1ABC", "text": "please ack me",
		})
		if status != http.StatusAccepted {
			t.Fatalf("compose: status=%d body=%s", status, string(body))
		}
		var resp dto.MessageResponse
		_ = json.Unmarshal(body, &resp)
		tctx.sink.waitForSubmit(t, 1, time.Second)
		tctx.sink.fireTxHook(0)
		tctx.waitForRow(t, resp.ID, time.Second, func(m *configstore.Message) bool {
			return m.SentAt != nil && m.AckState == messages.AckStateNone
		})

		// Soft-delete the outbound row mid-retry via the handler. This
		// calls Service.SoftDelete which cancels the retry schedule and
		// sets DeletedAt.
		req, err := http.NewRequest(http.MethodDelete,
			fmt.Sprintf("%s/api/messages/%d", tctx.srv.URL, resp.ID), nil)
		if err != nil {
			t.Fatalf("NewRequest DELETE: %v", err)
		}
		delResp, err := tctx.client.Do(req)
		if err != nil {
			t.Fatalf("DELETE: %v", err)
		}
		delResp.Body.Close()
		if delResp.StatusCode != http.StatusNoContent {
			t.Fatalf("DELETE: status=%d", delResp.StatusCode)
		}

		// Verify the row is soft-deleted.
		tombstoned := tctx.waitForRow(t, resp.ID, time.Second, func(m *configstore.Message) bool {
			return m.DeletedAt.Valid
		})
		if tombstoned.AckState != messages.AckStateNone {
			t.Logf("tombstone AckState = %q (acceptable if soft-delete closed the state)", tombstoned.AckState)
		}

		// Simulate the late ack arriving. The router's correlateAck
		// uses Unscoped() so it finds the soft-deleted row.
		tctx.msgSvc.Router().SendPacket(context.Background(), makeInboundAck(t, "W1ABC", "N0CALL", resp.MsgID))

		// Poll for AckedAt to flip while DeletedAt stays valid.
		deadline := time.Now().Add(time.Second)
		var got *configstore.Message
		for time.Now().Before(deadline) {
			rows, err := tctx.msgStore.FindOutstandingByMsgID(context.Background(), resp.MsgID, "W1ABC")
			if err != nil {
				t.Fatalf("FindOutstandingByMsgID: %v", err)
			}
			for i := range rows {
				if rows[i].ID == resp.ID {
					r := rows[i]
					got = &r
					break
				}
			}
			if got != nil && got.AckedAt != nil {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		if got == nil {
			t.Fatal("row never found via FindOutstandingByMsgID (Unscoped)")
		}
		if got.AckedAt == nil {
			t.Error("AckedAt never flipped on tombstoned row")
		}
		if !got.DeletedAt.Valid {
			t.Error("DeletedAt cleared on ack; expected tombstone to persist")
		}
		if got.AckState != messages.AckStateAcked {
			t.Errorf("AckState = %q, want acked", got.AckState)
		}
	})
}

