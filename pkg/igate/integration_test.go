package igate

import (
	"bufio"
	"context"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/aprs"
	"github.com/chrissnell/graywolf/pkg/igate/filters"
)

// mockServer is an in-process APRS-IS server stand-in. It accepts one
// connection, records the login, responds with a configurable logresp,
// and exposes hooks to send comments or traffic lines to the client.
type mockServer struct {
	ln net.Listener

	mu       sync.Mutex
	login    string
	lines    []string
	conn     net.Conn
	logresp  string // e.g. "# logresp KE7XYZ verified, server GWOLF"
	readyCh  chan struct{}
	clientMu sync.Mutex
}

func newMockServer(t *testing.T, logresp string) *mockServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	return &mockServer{ln: ln, logresp: logresp, readyCh: make(chan struct{})}
}

func (m *mockServer) addr() string { return m.ln.Addr().String() }

func (m *mockServer) close() { _ = m.ln.Close() }

func (m *mockServer) serve(t *testing.T) {
	t.Helper()
	conn, err := m.ln.Accept()
	if err != nil {
		return
	}
	m.mu.Lock()
	m.conn = conn
	m.mu.Unlock()
	// Write banner.
	_, _ = conn.Write([]byte("# graywolf mock aprs-is\r\n"))
	reader := bufio.NewReader(conn)
	line, err := reader.ReadString('\n')
	if err != nil {
		return
	}
	m.mu.Lock()
	m.login = strings.TrimRight(line, "\r\n")
	m.mu.Unlock()
	_, _ = conn.Write([]byte(m.logresp + "\r\n"))
	close(m.readyCh)
	// Drain subsequent lines (comments + RF->IS sends) until close.
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		m.mu.Lock()
		m.lines = append(m.lines, strings.TrimRight(line, "\r\n"))
		m.mu.Unlock()
	}
}

func (m *mockServer) writeToClient(s string) error {
	m.mu.Lock()
	conn := m.conn
	m.mu.Unlock()
	if conn == nil {
		return nil
	}
	_, err := conn.Write([]byte(s + "\r\n"))
	return err
}

func (m *mockServer) sentLines() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.lines))
	copy(out, m.lines)
	return out
}

func (m *mockServer) loginLine() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.login
}

func TestIgateLoginAndRFToIS(t *testing.T) {
	srv := newMockServer(t, "# logresp KE7XYZ verified, server MOCK")
	defer srv.close()
	go srv.serve(t)

	ig, err := New(Config{
		Server:          srv.addr(),
		StationCallsign: "KE7XYZ",
		ServerFilter:    "m/50",
		SoftwareName:    "graywolf-test",
		SoftwareVersion: "0.1",
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := ig.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer ig.Stop()

	// Wait for login.
	select {
	case <-srv.readyCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for login")
	}
	login := srv.loginLine()
	// Passcode is computed from the station callsign at login time
	// (APRSPasscode("KE7XYZ") == 22181). We no longer pass a passcode in.
	if !strings.Contains(login, "user KE7XYZ pass 22181") {
		t.Fatalf("unexpected login line: %q", login)
	}
	if !strings.Contains(login, "filter m/50") {
		t.Fatalf("login missing filter: %q", login)
	}

	// Wait for the connected state to propagate.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if ig.Status().Connected {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !ig.Status().Connected {
		t.Fatal("igate did not reach connected state")
	}

	// Simulate an RF->IS packet. We build a DecodedAPRSPacket with a
	// Raw info field so encodeTNC2 can extract the payload.
	raw := buildRawFrame(t, "W5ABC-7", "APRS", []string{"WIDE1-1"}, "!3725.00N/12158.00W>test")
	pkt := &aprs.DecodedAPRSPacket{
		Source: "W5ABC-7",
		Dest:   "APRS",
		Path:   []string{"WIDE1-1"},
		Raw:    raw,
		Type:   aprs.PacketPosition,
	}
	ig.gateRFToIS(pkt)

	// Allow the server goroutine to read the line.
	deadline = time.Now().Add(time.Second)
	var sent []string
	for time.Now().Before(deadline) {
		sent = srv.sentLines()
		if len(sent) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(sent) == 0 {
		t.Fatal("server did not receive any RF->IS line")
	}
	if !strings.Contains(sent[0], "W5ABC-7>APRS,WIDE1-1,qAR,KE7XYZ") {
		t.Fatalf("unexpected line: %q", sent[0])
	}
}

func TestIgateFiltersAndForwardsIStoRF(t *testing.T) {
	srv := newMockServer(t, "# logresp KE7XYZ verified")
	defer srv.close()
	go srv.serve(t)

	// No governor — IS->RF will increment "filtered"/downlinked stats
	// based solely on filter rules. Use an in-memory Filter allowing
	// everything from W5*.
	ig, err := New(Config{
		Server:          srv.addr(),
		StationCallsign: "KE7XYZ",
		Rules: []filters.Rule{
			{ID: 1, Priority: 10, Type: filters.TypePrefix, Pattern: "W5", Action: filters.Allow},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := ig.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer ig.Stop()

	<-srv.readyCh

	// Feed two IS lines: one from an allowed source, one denied.
	_ = srv.writeToClient("W5ABC-7>APRS,TCPIP*,qAC,T2:!3725.00N/12158.00W>hi")
	_ = srv.writeToClient("N0BAD>APRS,TCPIP*,qAC,T2:!3725.00N/12158.00W>nope")

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		s := ig.Status()
		if s.Filtered >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	status := ig.Status()
	if status.Filtered < 1 {
		t.Fatalf("expected at least 1 filtered packet, got %d", status.Filtered)
	}
}

func TestIgatePartialAuthDropTriggersReconnect(t *testing.T) {
	// First server: verified logresp, then injects an unverified
	// logresp comment to trigger reconnect.
	srv := newMockServer(t, "# logresp KE7XYZ verified")
	defer srv.close()
	go srv.serve(t)

	ig, err := New(Config{
		Server:          srv.addr(),
		StationCallsign: "KE7XYZ",
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := ig.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer ig.Stop()

	<-srv.readyCh

	// Send a post-login unverified logresp; client should drop.
	_ = srv.writeToClient("# logresp KE7XYZ unverified, server MOCK")

	// Give the read loop a moment to process and flip to disconnected.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if !ig.Status().Connected {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("iGate did not drop connection on partial auth drop")
}

func TestIgateSimulationModeSkipsWrite(t *testing.T) {
	srv := newMockServer(t, "# logresp KE7XYZ verified")
	defer srv.close()
	go srv.serve(t)

	ig, err := New(Config{
		Server:          srv.addr(),
		StationCallsign: "KE7XYZ",
		SimulationMode:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := ig.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer ig.Stop()

	<-srv.readyCh
	// Wait for connected gauge.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if ig.Status().Connected {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	raw := buildRawFrame(t, "W5ABC-7", "APRS", nil, "!3725.00N/12158.00W>sim")
	pkt := &aprs.DecodedAPRSPacket{
		Source: "W5ABC-7",
		Dest:   "APRS",
		Raw:    raw,
		Type:   aprs.PacketPosition,
	}
	ig.gateRFToIS(pkt)

	time.Sleep(100 * time.Millisecond)
	if got := srv.sentLines(); len(got) != 0 {
		t.Fatalf("simulation mode should not write to server, got: %v", got)
	}
	if ig.Status().Gated != 1 {
		t.Fatalf("expected gated=1 in simulation, got %d", ig.Status().Gated)
	}

	// Toggle off and send again — should now hit the wire.
	if err := ig.SetSimulationMode(false); err != nil {
		t.Fatal(err)
	}
	// Different payload to avoid dedup.
	raw2 := buildRawFrame(t, "W5ABC-7", "APRS", nil, "!3725.00N/12158.00W>live")
	pkt2 := &aprs.DecodedAPRSPacket{
		Source: "W5ABC-7",
		Dest:   "APRS",
		Raw:    raw2,
		Type:   aprs.PacketPosition,
	}
	ig.gateRFToIS(pkt2)
	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if len(srv.sentLines()) > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("live-mode RF->IS send did not reach server")
}
