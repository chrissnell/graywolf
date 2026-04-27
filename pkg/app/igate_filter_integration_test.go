package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/igate"
	"github.com/chrissnell/graywolf/pkg/metrics"
	"github.com/chrissnell/graywolf/pkg/webapi"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

// Integration tests for auto-appended tactical-callsign clauses on the
// APRS-IS server-login filter. Mutations are driven through the real
// webapi handlers; the mock APRS-IS server captures each login line on a
// buffered channel so tests sync via waitForLogin rather than sleep-polling.
type tfMockAprsIsServer struct {
	ln        net.Listener
	logresp   string
	logins    chan string
	closedMu  sync.Mutex
	closed    bool
	wg        sync.WaitGroup
	closeOnce sync.Once
}

func newTFMockAprsIsServer(t *testing.T, logresp string) *tfMockAprsIsServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	return &tfMockAprsIsServer{
		ln:      ln,
		logresp: logresp,
		logins:  make(chan string, 16),
	}
}

func (m *tfMockAprsIsServer) addr() string { return m.ln.Addr().String() }

// start begins the accept loop in a goroutine. Safe to call once.
func (m *tfMockAprsIsServer) start() {
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		for {
			conn, err := m.ln.Accept()
			if err != nil {
				return
			}
			m.wg.Add(1)
			go m.handle(conn)
		}
	}()
}

// handle serves one connection: banner, read login, reply logresp, drain
// any subsequent lines quietly until the client closes or the server is
// closed. Login is pushed onto m.logins before the logresp is written so
// the test-observable event precedes the client's "connected" transition.
func (m *tfMockAprsIsServer) handle(conn net.Conn) {
	defer m.wg.Done()
	defer conn.Close()

	_, _ = conn.Write([]byte("# graywolf tf-mock aprs-is\r\n"))
	reader := bufio.NewReader(conn)
	// A bounded read deadline ensures a stalled client can't pin this
	// goroutine forever; the read is dominated by login arrival which is
	// sub-second in healthy flow.
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	line, err := reader.ReadString('\n')
	if err != nil {
		return
	}
	login := strings.TrimRight(line, "\r\n")

	m.closedMu.Lock()
	if !m.closed {
		// Non-blocking send; the buffered channel absorbs bursts and we
		// don't want a slow test to stall the server's accept loop.
		select {
		case m.logins <- login:
		default:
		}
	}
	m.closedMu.Unlock()

	_, _ = conn.Write([]byte(m.logresp + "\r\n"))

	// Keep the session alive: drain any trailing lines until close /
	// EOF. No deadline here — the client owns the session lifetime.
	_ = conn.SetReadDeadline(time.Time{})
	for {
		if _, err := reader.ReadString('\n'); err != nil {
			return
		}
	}
}

// close stops accepting new connections and signals drains to exit.
// Safe to call multiple times.
func (m *tfMockAprsIsServer) close() {
	m.closeOnce.Do(func() {
		m.closedMu.Lock()
		m.closed = true
		m.closedMu.Unlock()
		_ = m.ln.Close()
		m.wg.Wait()
	})
}

// waitForLogin returns the next captured login line or fails the test if
// no login arrives within timeout. Uses a channel receive — no sleep
// polling.
func (m *tfMockAprsIsServer) waitForLogin(t *testing.T, timeout time.Duration) string {
	t.Helper()
	select {
	case login := <-m.logins:
		return login
	case <-time.After(timeout):
		t.Fatalf("timeout waiting for APRS-IS login within %v", timeout)
		return ""
	}
}

// waitForNoLogin asserts that no login arrives within the window. Returns
// the offending login for diagnostic output if one was seen.
func (m *tfMockAprsIsServer) waitForNoLogin(t *testing.T, window time.Duration) {
	t.Helper()
	select {
	case login := <-m.logins:
		t.Fatalf("unexpected APRS-IS login during %v: %q", window, login)
	case <-time.After(window):
		return
	}
}

// --- harness ---------------------------------------------------------------

// tfHarness bundles the integration fixture used by every subtest. Each
// subtest builds its own to isolate state (tacticals, login channel,
// last-applied filter, metric counter).
type tfHarness struct {
	ctx        context.Context
	cancel     context.CancelFunc
	mock       *tfMockAprsIsServer
	app        *App
	ig         *igate.Igate
	reloadDone sync.WaitGroup
	httpSrv    *httptest.Server
	tb         testing.TB
	// Counter value captured at startup; all "metric incremented by 1"
	// assertions subtract this to survive prior mutations in the harness.
	baselineRecompositions float64
}

// tfSetup wires an in-memory configstore, a real *igate.Igate talking to
// the mock APRS-IS server, a webapi.Server exposing the tactical CRUD
// routes (via httptest.Server), and a reload drainer goroutine that calls
// the production App.reloadIgate on each signal.
//
// The returned harness is ready for mutation + waitForLogin. Callers must
// defer h.close() to tear everything down.
func tfSetup(t *testing.T, baseFilter string, preseed []configstore.TacticalCallsign) *tfHarness {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())

	// Discarding logger — reload debug lines would otherwise flood -v.
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	store, err := configstore.OpenMemory()
	if err != nil {
		cancel()
		t.Fatalf("OpenMemory: %v", err)
	}

	// Seed the station identity; resolveOurCall in the webapi handlers
	// reads this, and the tactical collision guard checks against it.
	if err := store.UpsertStationConfig(ctx, configstore.StationConfig{Callsign: "KE7XYZ"}); err != nil {
		cancel()
		t.Fatalf("UpsertStationConfig: %v", err)
	}
	// Seed IGateConfig with the operator's base filter. The transport
	// fields (server, port) are rewritten below once the mock is bound
	// to a port.
	if err := store.UpsertIGateConfig(ctx, &configstore.IGateConfig{
		Enabled:      true,
		Server:       "placeholder",
		Port:         14580,
		TxChannel:    1,
		RfChannel:    1,
		MaxMsgHops:   2,
		ServerFilter: baseFilter,
	}); err != nil {
		cancel()
		t.Fatalf("UpsertIGateConfig: %v", err)
	}
	for _, tc := range preseed {
		if err := store.CreateTacticalCallsign(ctx, &tc); err != nil {
			cancel()
			t.Fatalf("CreateTacticalCallsign %q: %v", tc.Callsign, err)
		}
	}

	mock := newTFMockAprsIsServer(t, "# logresp KE7XYZ verified, server TFMOCK")
	mock.start()

	// Rewrite the IGateConfig's transport to point at the mock. We
	// re-read and re-upsert so every other field is preserved.
	igCfg, err := store.GetIGateConfig(ctx)
	if err != nil || igCfg == nil {
		cancel()
		t.Fatalf("GetIGateConfig: %v (cfg=%v)", err, igCfg)
	}
	host, portStr, _ := net.SplitHostPort(mock.addr())
	var port int
	_, _ = fmt.Sscanf(portStr, "%d", &port)
	igCfg.Server = host
	igCfg.Port = uint32(port)
	if err := store.UpsertIGateConfig(ctx, igCfg); err != nil {
		cancel()
		t.Fatalf("UpsertIGateConfig (rewrite transport): %v", err)
	}

	// Compose the initial filter via the production helper so the mock
	// sees the same first-login wire value that wireIGate would have
	// produced.
	composed, err := buildIgateFilter(ctx, store)
	if err != nil {
		cancel()
		t.Fatalf("buildIgateFilter: %v", err)
	}

	m := metrics.New()

	ig, err := igate.New(igate.Config{
		Server:          mock.addr(),
		StationCallsign: "KE7XYZ",
		ServerFilter:    composed,
		SoftwareName:    "graywolf-test",
		SoftwareVersion: "phase4",
		Logger:          logger,
		Registry:        m.Registry,
	})
	if err != nil {
		cancel()
		t.Fatalf("igate.New: %v", err)
	}

	a := &App{
		cfg:                    DefaultConfig(),
		logger:                 logger,
		store:                  store,
		metrics:                m,
		ig:                     ig,
		igateReload:            make(chan struct{}, 1),
		lastAppliedIgateFilter: composed,
	}

	// Start the igate supervisor so the first connection (and any
	// subsequent reconnects triggered by Reconfigure) actually reach
	// the mock.
	if err := ig.Start(ctx); err != nil {
		cancel()
		t.Fatalf("ig.Start: %v", err)
	}

	// Webapi server with the tactical CRUD routes mounted. The
	// messagesService is intentionally left nil — tactical handlers
	// nil-guard it, and leaving it nil keeps the harness minimal
	// (no full messages.Service spin-up).
	apiSrv, err := webapi.NewServer(webapi.Config{
		Store:  store,
		Logger: logger,
	})
	if err != nil {
		cancel()
		t.Fatalf("webapi.NewServer: %v", err)
	}
	apiSrv.SetMessagesReload(make(chan struct{}, 1)) // drained to /dev/null
	apiSrv.SetIgateReload(a.igateReload)
	mux := http.NewServeMux()
	apiSrv.RegisterRoutes(mux)
	httpSrv := httptest.NewServer(mux)

	h := &tfHarness{
		ctx:     ctx,
		cancel:  cancel,
		mock:    mock,
		app:     a,
		ig:      ig,
		httpSrv: httpSrv,
		tb:      t,
	}

	// Drainer goroutine: mirrors pkg/app's igateComponent reload loop.
	// Runs the real App.reloadIgate on every signal, so the production
	// buildIgateFilter + last-applied no-op skip + metric increment all
	// exercise on the real paths.
	h.reloadDone.Add(1)
	go func() {
		defer h.reloadDone.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case _, ok := <-a.igateReload:
				if !ok {
					return
				}
				a.reloadIgate(ctx)
			}
		}
	}()

	// Baseline: wait for the first login so every subtest starts from a
	// known post-startup state.
	h.mock.waitForLogin(t, 5*time.Second)
	h.baselineRecompositions = testutil.ToFloat64(m.IgateFilterRecompositions)

	return h
}

func (h *tfHarness) close() {
	h.httpSrv.Close()
	h.ig.Stop()
	h.cancel()
	h.reloadDone.Wait()
	h.mock.close()
	if h.app.store != nil {
		if err := h.app.store.Close(); err != nil {
			h.tb.Logf("configstore close: %v", err)
		}
	}
}

// --- HTTP helpers ----------------------------------------------------------

func (h *tfHarness) doJSON(t *testing.T, method, path string, body any) (int, []byte) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode: %v", err)
		}
	}
	req, err := http.NewRequest(method, h.httpSrv.URL+path, &buf)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := h.httpSrv.Client().Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, data
}

// createTactical POSTs a fresh enabled tactical and returns the server-
// assigned id. Fails the test on non-201 or decode error.
func (h *tfHarness) createTactical(t *testing.T, callsign string, enabled bool) uint32 {
	t.Helper()
	code, body := h.doJSON(t, http.MethodPost, "/api/messages/tactical",
		dto.TacticalCallsignRequest{Callsign: callsign, Enabled: enabled})
	if code != http.StatusCreated {
		t.Fatalf("create %s: status=%d body=%s", callsign, code, string(body))
	}
	var resp dto.TacticalCallsignResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode create response: %v (body=%s)", err, string(body))
	}
	return resp.ID
}

// updateTactical sends a PUT with the given full-row body. The handler is
// a replace-style PUT, not a PATCH, so the caller constructs the entire
// desired post-update state.
func (h *tfHarness) updateTactical(t *testing.T, id uint32, req dto.TacticalCallsignRequest) {
	t.Helper()
	code, body := h.doJSON(t, http.MethodPut, fmt.Sprintf("/api/messages/tactical/%d", id), req)
	if code != http.StatusOK {
		t.Fatalf("update %d (%s): status=%d body=%s", id, req.Callsign, code, string(body))
	}
}

// deleteTactical removes a row by id.
func (h *tfHarness) deleteTactical(t *testing.T, id uint32) {
	t.Helper()
	code, body := h.doJSON(t, http.MethodDelete, fmt.Sprintf("/api/messages/tactical/%d", id), nil)
	if code != http.StatusNoContent {
		t.Fatalf("delete %d: status=%d body=%s", id, code, string(body))
	}
}

// acceptInvite posts to /api/tacticals — the bonus seventh mutation path.
func (h *tfHarness) acceptInvite(t *testing.T, callsign string) {
	t.Helper()
	code, body := h.doJSON(t, http.MethodPost, "/api/tacticals",
		dto.AcceptInviteRequest{Callsign: callsign})
	if code != http.StatusOK {
		t.Fatalf("accept invite %s: status=%d body=%s", callsign, code, string(body))
	}
}

// --- metric helpers --------------------------------------------------------

// expectRecompositionDelta asserts that the recompositions counter has
// advanced by exactly `want` since the harness baseline. Takes a short
// deadline because the reload goroutine runs asynchronously; the metric
// is bumped inside reloadIgate AFTER Reconfigure returns, which is after
// the next login we already observed — so in practice the counter is
// already up by the time we call this. The deadline guards a race where
// the test reads the counter before reloadIgate has returned.
func (h *tfHarness) expectRecompositionDelta(t *testing.T, want float64) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var got float64
	for {
		got = testutil.ToFloat64(h.app.metrics.IgateFilterRecompositions) - h.baselineRecompositions
		if got == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("recompositions delta: got %v, want %v", got, want)
		}
		// Small sleep here is acceptable: the loop is bounded by a
		// hard deadline, and the metric is a side-effect of Reconfigure
		// returning — there is no signal we can directly await.
		time.Sleep(5 * time.Millisecond)
	}
}

// --- filter extraction helpers --------------------------------------------

// extractFilter returns the "filter ..." tail of an APRS-IS login line, or
// the empty string if no filter keyword is present. The login format is:
//
//	user CALL pass NNN vers NAME VER filter F...
//
// "filter" is the last keyword, so everything after it is the filter body.
func extractFilter(login string) string {
	_, after, ok := strings.Cut(login, " filter ")
	if !ok {
		return ""
	}
	return after
}

// --- the test matrix -------------------------------------------------------

// TestIgateFilterIntegration_TacticalMutations runs the Phase 4 mutation
// matrix. Each subtest provisions its own harness so state does not leak
// across cases.
//
// Subtests do NOT t.Parallel() because configstore.OpenMemory() uses
// the `file::memory:?cache=shared` DSN — every call in the same process
// resolves to the same SQLite in-memory instance, so parallel subtests
// would share tactical rows and iGate config. Each sub-test instead
// stands up its own harness sequentially; total runtime is bounded by
// the igate backoff's 1s Initial * N reconnects (~10-12 s for the full
// matrix).
func TestIgateFilterIntegration_TacticalMutations(t *testing.T) {
	// Case 1: create an enabled tactical → next login has g/T1.
	t.Run("CreateEnabled_AppendsGClause", func(t *testing.T) {
		h := tfSetup(t, "m/50", nil)
		defer h.close()

		h.createTactical(t, "T1", true)
		login := h.mock.waitForLogin(t, 5*time.Second)
		filt := extractFilter(login)
		if filt != "m/50 g/T1" {
			t.Fatalf("filter: got %q, want %q", filt, "m/50 g/T1")
		}
		h.expectRecompositionDelta(t, 1)
	})

	// Case 2: update non-callsign field (alias) on an already-enabled
	// tactical — composed filter is byte-identical, so the no-op skip
	// must suppress the reconnect (no new login arrives).
	t.Run("UpdateAlias_NoReconnect", func(t *testing.T) {
		h := tfSetup(t, "m/50", []configstore.TacticalCallsign{
			{Callsign: "ALPHA", Enabled: true},
		})
		defer h.close()

		// Sanity: the baseline login already contained ALPHA.
		// Fetch the row so we know its id.
		rows, err := h.app.store.ListTacticalCallsigns(h.ctx)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(rows) != 1 {
			t.Fatalf("expected 1 seeded row, got %d", len(rows))
		}
		id := rows[0].ID

		// Alias-only update. Enabled and Callsign unchanged.
		h.updateTactical(t, id, dto.TacticalCallsignRequest{
			Callsign: "ALPHA",
			Alias:    "Alpha team",
			Enabled:  true,
		})

		// The signal fires, reloadIgate runs, sees composed ==
		// lastApplied, and returns without logging or bumping the
		// metric. No reconnect fires → mock observes no second login.
		h.mock.waitForNoLogin(t, 750*time.Millisecond)
		h.expectRecompositionDelta(t, 0)
	})

	// Case 3: rename a tactical — next login has the new name and NOT
	// the old. Exercises the same PUT handler as case 2 but with a
	// different composed result, so reload crosses the no-op skip.
	t.Run("Rename_ReplacesInFilter", func(t *testing.T) {
		h := tfSetup(t, "", []configstore.TacticalCallsign{
			{Callsign: "OLDNAME", Enabled: true},
		})
		defer h.close()

		rows, _ := h.app.store.ListTacticalCallsigns(h.ctx)
		id := rows[0].ID

		h.updateTactical(t, id, dto.TacticalCallsignRequest{
			Callsign: "NEWNAME",
			Enabled:  true,
		})
		login := h.mock.waitForLogin(t, 5*time.Second)
		filt := extractFilter(login)
		if !strings.Contains(filt, "NEWNAME") {
			t.Fatalf("expected NEWNAME in filter; got %q", filt)
		}
		if strings.Contains(filt, "OLDNAME") {
			t.Fatalf("unexpected OLDNAME still in filter: %q", filt)
		}
		h.expectRecompositionDelta(t, 1)
	})

	// Case 4: disable an enabled tactical — next login drops it.
	t.Run("Disable_RemovesFromFilter", func(t *testing.T) {
		h := tfSetup(t, "m/50", []configstore.TacticalCallsign{
			{Callsign: "DROPME", Enabled: true},
		})
		defer h.close()

		rows, _ := h.app.store.ListTacticalCallsigns(h.ctx)
		id := rows[0].ID

		h.updateTactical(t, id, dto.TacticalCallsignRequest{
			Callsign: "DROPME",
			Enabled:  false,
		})
		login := h.mock.waitForLogin(t, 5*time.Second)
		filt := extractFilter(login)
		if strings.Contains(filt, "DROPME") {
			t.Fatalf("expected DROPME absent from filter; got %q", filt)
		}
		if filt != "m/50" {
			t.Fatalf("filter: got %q, want %q", filt, "m/50")
		}
		h.expectRecompositionDelta(t, 1)
	})

	// Case 5: enable a previously-disabled tactical — next login gains it.
	t.Run("Enable_AddsToFilter", func(t *testing.T) {
		h := tfSetup(t, "m/50", []configstore.TacticalCallsign{
			{Callsign: "READD", Enabled: false},
		})
		defer h.close()

		rows, _ := h.app.store.ListTacticalCallsigns(h.ctx)
		id := rows[0].ID

		h.updateTactical(t, id, dto.TacticalCallsignRequest{
			Callsign: "READD",
			Enabled:  true,
		})
		login := h.mock.waitForLogin(t, 5*time.Second)
		filt := extractFilter(login)
		if filt != "m/50 g/READD" {
			t.Fatalf("filter: got %q, want %q", filt, "m/50 g/READD")
		}
		h.expectRecompositionDelta(t, 1)
	})

	// Case 6: delete a tactical — next login drops it.
	t.Run("Delete_RemovesFromFilter", func(t *testing.T) {
		h := tfSetup(t, "", []configstore.TacticalCallsign{
			{Callsign: "GONE", Enabled: true},
		})
		defer h.close()

		rows, _ := h.app.store.ListTacticalCallsigns(h.ctx)
		id := rows[0].ID

		h.deleteTactical(t, id)
		login := h.mock.waitForLogin(t, 5*time.Second)
		filt := extractFilter(login)
		// Base was empty and only tactical is gone — buildLogin
		// substitutes the no-match sentinel for the empty filter.
		if filt != "r/-48.87/-27.14/0" {
			t.Fatalf("filter: got %q, want no-match sentinel", filt)
		}
		h.expectRecompositionDelta(t, 1)
	})

	// Case 7: operator manually lists OPX in base; adding a tactical
	// OPX must NOT duplicate it in the composed filter. Dedup is
	// case-insensitive (Phase 1 contract).
	t.Run("ManualGClause_NotDuplicated", func(t *testing.T) {
		h := tfSetup(t, "g/OPX m/100", nil)
		defer h.close()

		h.createTactical(t, "OPX", true)
		// The create fires a reload. If the composed filter equals the
		// last-applied (because OPX was already covered by base), the
		// no-op skip fires and we see no new login.
		h.mock.waitForNoLogin(t, 750*time.Millisecond)
		h.expectRecompositionDelta(t, 0)

		// Sanity: the composed filter we would emit is identical to the
		// baseline. Verified by re-composing directly.
		got, err := buildIgateFilter(h.ctx, h.app.store)
		if err != nil {
			t.Fatalf("buildIgateFilter: %v", err)
		}
		if got != "g/OPX m/100" {
			t.Fatalf("composed filter: got %q, want %q", got, "g/OPX m/100")
		}
	})

	// Case 8: empty base + no tacticals → initial login uses the
	// no-match sentinel. Then adding the first tactical transitions
	// sentinel → g/T1.
	t.Run("EmptyBase_SentinelThenTransition", func(t *testing.T) {
		h := tfSetup(t, "", nil)
		defer h.close()

		// The setup already awaited the first login. We have to look at
		// it, but setup didn't expose it — so we inspect the recently
		// captured login via a fresh reconnect trigger using an
		// unrelated reload signal? That would defeat the test.
		//
		// Simpler: the baseline login is gone from the buffer already,
		// but we know what it must have been, and we asserted via
		// buildIgateFilter's empty-in contract plus client.go's
		// buildLogin sentinel substitution. Re-derive here to prove the
		// contract: call buildLogin with an empty composed filter and
		// confirm it substitutes.
		composed, err := buildIgateFilter(h.ctx, h.app.store)
		if err != nil {
			t.Fatalf("buildIgateFilter (pre-tactical): %v", err)
		}
		if composed != "" {
			t.Fatalf("expected empty composed pre-tactical; got %q", composed)
		}
		// Now add the first tactical. buildLogin sees a non-empty
		// composed string this time, so the sentinel is replaced by
		// g/T1 on the next login.
		h.createTactical(t, "T1", true)
		login := h.mock.waitForLogin(t, 5*time.Second)
		filt := extractFilter(login)
		if filt != "g/T1" {
			t.Fatalf("post-first-tactical filter: got %q, want %q", filt, "g/T1")
		}
		// And the initial login (captured during setup and consumed by
		// setup's waitForLogin) must not contain any g/ clause — we
		// verify via the setup-time compose that it was empty → the
		// sentinel substitution lives in client.go:buildLogin.
		// Second-login captures the transition; the explicit check on
		// the post-transition filter proves it replaced "".
		h.expectRecompositionDelta(t, 1)
	})

	// Case 8b: a dedicated check that the first login on a stone-cold
	// empty configuration is the no-match sentinel. We stand up a fresh
	// harness and inspect the FIRST login BEFORE any mutation, which
	// setup drains into a separate channel.
	t.Run("EmptyBase_FirstLoginIsSentinel", func(t *testing.T) {
		// Mirror tfSetup but capture the first login inline rather than
		// draining it.
		ctx := t.Context()
		logger := slog.New(slog.NewTextHandler(io.Discard, nil))
		store, err := configstore.OpenMemory()
		if err != nil {
			t.Fatalf("OpenMemory: %v", err)
		}
		defer store.Close()
		if err := store.UpsertStationConfig(ctx, configstore.StationConfig{Callsign: "KE7XYZ"}); err != nil {
			t.Fatalf("UpsertStationConfig: %v", err)
		}
		if err := store.UpsertIGateConfig(ctx, &configstore.IGateConfig{
			Enabled: true, Server: "placeholder", Port: 1, ServerFilter: "",
		}); err != nil {
			t.Fatalf("UpsertIGateConfig: %v", err)
		}
		mock := newTFMockAprsIsServer(t, "# logresp KE7XYZ verified")
		mock.start()
		defer mock.close()

		composed, err := buildIgateFilter(ctx, store)
		if err != nil {
			t.Fatalf("buildIgateFilter: %v", err)
		}
		if composed != "" {
			t.Fatalf("expected empty composed; got %q", composed)
		}
		ig, err := igate.New(igate.Config{
			Server:          mock.addr(),
			StationCallsign: "KE7XYZ",
			ServerFilter:    composed,
			Logger:          logger,
		})
		if err != nil {
			t.Fatalf("igate.New: %v", err)
		}
		if err := ig.Start(ctx); err != nil {
			t.Fatalf("ig.Start: %v", err)
		}
		defer ig.Stop()

		login := mock.waitForLogin(t, 5*time.Second)
		filt := extractFilter(login)
		if filt != "r/-48.87/-27.14/0" {
			t.Fatalf("first login filter: got %q, want sentinel", filt)
		}
	})

	// Case 9: accept-invite path — the bonus seventh mutation from
	// Phase 3's audit. Validates the previously-latent bug (accept-invite
	// only fired messagesReload) is now fixed: the g/ clause appears on
	// the next login.
	t.Run("AcceptInvite_AppendsGClause", func(t *testing.T) {
		h := tfSetup(t, "m/50", nil)
		defer h.close()

		h.acceptInvite(t, "INVITED")
		login := h.mock.waitForLogin(t, 5*time.Second)
		filt := extractFilter(login)
		if filt != "m/50 g/INVITED" {
			t.Fatalf("filter: got %q, want %q", filt, "m/50 g/INVITED")
		}
		h.expectRecompositionDelta(t, 1)
	})

	// Additional check: multi-tactical sort determinism. buildIgateFilter
	// sorts lexically before compose, so three tacticals added in an
	// arbitrary order must produce a sorted g/ clause.
	t.Run("MultipleTacticals_SortedLexically", func(t *testing.T) {
		h := tfSetup(t, "", nil)
		defer h.close()

		// Create in reverse-alphabetical order on purpose.
		h.createTactical(t, "ZULU", true)
		h.mock.waitForLogin(t, 5*time.Second)
		h.createTactical(t, "ALPHA", true)
		h.mock.waitForLogin(t, 5*time.Second)
		h.createTactical(t, "MIKE", true)
		login := h.mock.waitForLogin(t, 5*time.Second)
		filt := extractFilter(login)
		want := "g/ALPHA/MIKE/ZULU"
		if filt != want {
			t.Fatalf("filter: got %q, want %q", filt, want)
		}
	})
}

