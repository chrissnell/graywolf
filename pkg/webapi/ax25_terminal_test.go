package webapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/chrissnell/graywolf/pkg/ax25"
	"github.com/chrissnell/graywolf/pkg/ax25conn"
	"github.com/chrissnell/graywolf/pkg/ax25termws"
	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/txgovernor"
	"github.com/chrissnell/graywolf/pkg/webauth"
)

// nopTxSink discards every frame. Used to spin up a real
// ax25conn.Manager without dragging in the txgovernor wiring.
type nopTxSink struct{}

func (nopTxSink) Submit(_ context.Context, _ uint32, _ *ax25.Frame, _ txgovernor.SubmitSource) error {
	return nil
}

type ax25TermFixture struct {
	srv       *Server
	mgr       *ax25conn.Manager
	mux       http.Handler
	authStore *webauth.AuthStore
	token     string
	wsURL     string
	ts        *httptest.Server
}

func newAX25TerminalFixture(t *testing.T, withManager bool) *ax25TermFixture {
	t.Helper()
	store, err := configstore.OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	authStore, err := webauth.NewAuthStore(store.DB())
	if err != nil {
		t.Fatalf("NewAuthStore: %v", err)
	}
	token := seedUserAndSession(t, authStore)

	silent := slog.New(slog.NewTextHandler(io.Discard, nil))
	apiSrv, err := NewServer(Config{
		Store:   store,
		KissCtx: context.Background(),
		Logger:  silent,
		Version: "test",
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	var mgr *ax25conn.Manager
	if withManager {
		mgr = ax25conn.NewManager(ax25conn.ManagerConfig{TxSink: nopTxSink{}, Logger: silent})
		t.Cleanup(mgr.Close)
		apiSrv.SetAX25Manager(mgr)
	}

	apiMux := http.NewServeMux()
	apiSrv.RegisterRoutes(apiMux)
	outer := http.NewServeMux()
	outer.Handle("/api/", webauth.RequireAuth(authStore)(apiMux))

	ts := httptest.NewServer(outer)
	t.Cleanup(ts.Close)
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/ax25/terminal"

	return &ax25TermFixture{
		srv:       apiSrv,
		mgr:       mgr,
		mux:       outer,
		authStore: authStore,
		token:     token,
		wsURL:     wsURL,
		ts:        ts,
	}
}

func dialAuth(t *testing.T, ctx context.Context, wsURL, token string) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	hdr := http.Header{}
	hdr.Set("Cookie", "graywolf_session="+token)
	return websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: hdr})
}

func TestAX25Terminal_RejectsUnauthenticated(t *testing.T) {
	fx := newAX25TerminalFixture(t, true)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, resp, err := websocket.Dial(ctx, fx.wsURL, nil)
	if err == nil {
		conn.CloseNow()
		t.Fatal("expected unauthenticated dial to fail")
	}
	if resp == nil {
		t.Fatalf("expected http response, got err only: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestAX25Terminal_ServiceUnavailableWhenNoManager(t *testing.T) {
	fx := newAX25TerminalFixture(t, false)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, resp, err := dialAuth(t, ctx, fx.wsURL, fx.token)
	if err == nil {
		conn.CloseNow()
		t.Fatal("expected dial to fail when manager is nil")
	}
	if resp == nil {
		t.Fatalf("expected http response, got err only: %v", err)
	}
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", resp.StatusCode)
	}
}

func TestAX25Terminal_RoundTripPing(t *testing.T) {
	fx := newAX25TerminalFixture(t, true)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, resp, err := dialAuth(t, ctx, fx.wsURL, fx.token)
	if err != nil {
		t.Fatalf("dial: %v (status=%d)", err, statusOrZero(resp))
	}
	defer c.CloseNow()
	// coder/websocket processes incoming pong frames inside Read on
	// the client side, so Ping() blocks waiting for pong unless
	// something is draining the client connection. Run a background
	// reader to keep the protocol moving while Ping waits.
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		for {
			if _, _, err := c.Read(ctx); err != nil {
				return
			}
		}
	}()
	pingCtx, pingCancel := context.WithTimeout(ctx, 2*time.Second)
	defer pingCancel()
	if err := c.Ping(pingCtx); err != nil {
		t.Fatalf("ping: %v", err)
	}
	if err := c.Close(websocket.StatusNormalClosure, "done"); err != nil {
		t.Fatalf("close: %v", err)
	}
	<-readerDone
}

func TestAX25Terminal_ConnectOpensSession(t *testing.T) {
	fx := newAX25TerminalFixture(t, true)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := dialAuth(t, ctx, fx.wsURL, fx.token)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.CloseNow()

	connect := ax25termws.Envelope{Kind: ax25termws.KindConnect, Connect: &ax25termws.ConnectArgs{
		ChannelID: 1, LocalCall: "K0SWE", LocalSSID: 1, DestCall: "BBS", DestSSID: 3,
	}}
	payload, _ := json.Marshal(connect)
	if err := c.Write(ctx, websocket.MessageText, payload); err != nil {
		t.Fatalf("write connect: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if fx.mgr.Count() >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if fx.mgr.Count() != 1 {
		t.Fatalf("expected 1 active session, got %d", fx.mgr.Count())
	}

	// Read at least one envelope back -- the session should emit a
	// state change envelope when leaving DISCONNECTED.
	rdCtx, rdCancel := context.WithTimeout(ctx, 2*time.Second)
	defer rdCancel()
	_, data, err := c.Read(rdCtx)
	if err != nil {
		t.Fatalf("read state envelope: %v", err)
	}
	var env ax25termws.Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatalf("unmarshal: %v (raw=%q)", err, data)
	}
	if env.Kind != ax25termws.KindState {
		t.Fatalf("expected state envelope, got %+v", env)
	}
}

func statusOrZero(r *http.Response) int {
	if r == nil {
		return 0
	}
	return r.StatusCode
}
