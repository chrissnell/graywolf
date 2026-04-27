package agw

import (
	"context"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/internal/testtx"
)

// fakeSink embeds the shared testtx.Recorder and adds a per-submit
// signal channel so tests can block until a frame has arrived
// without polling on Len().
type fakeSink struct {
	*testtx.Recorder
	ch chan struct{}
}

func newFakeSink() *fakeSink {
	s := &fakeSink{
		Recorder: testtx.NewRecorder(),
		ch:       make(chan struct{}, 16),
	}
	s.OnSubmit(func(testtx.Capture) { s.ch <- struct{}{} })
	return s
}

func TestAGWVersionAndSendUnproto(t *testing.T) {
	sink := newFakeSink()
	srv := NewServer(ServerConfig{
		ListenAddr:    "127.0.0.1:0",
		PortCallsigns: []string{"N0CALL-1"},
		Sink:          sink,
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv.cfg.ListenAddr = ln.Addr().String()
	_ = ln.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serveDone := make(chan error, 1)
	go func() { serveDone <- srv.ListenAndServe(ctx) }()

	// Connect with retry.
	var conn net.Conn
	for i := 0; i < 50; i++ {
		c, err := net.Dial("tcp", srv.cfg.ListenAddr)
		if err == nil {
			conn = c
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if conn == nil {
		t.Fatal("could not connect")
	}
	defer conn.Close()

	// Ask for version.
	if err := WriteFrame(conn, &Header{DataKind: KindVersion}, nil); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	h, data, err := ReadFrame(conn)
	if err != nil {
		t.Fatal(err)
	}
	if h.DataKind != KindVersion || len(data) != 8 {
		t.Errorf("bad version response: %+v", h)
	}

	// Send an UNPROTO UI frame.
	if err := WriteFrame(conn, &Header{
		DataKind: KindSendUnproto,
		PID:      0xF0,
		CallFrom: "W1AW",
		CallTo:   "APRS",
	}, []byte("hello world")); err != nil {
		t.Fatal(err)
	}

	select {
	case <-sink.ch:
	case <-time.After(2 * time.Second):
		t.Fatal("sink did not receive frame")
	}
	f := sink.Frames()[0]
	if f.Source.Call != "W1AW" || f.Dest.Call != "APRS" {
		t.Errorf("addrs: %+v / %+v", f.Source, f.Dest)
	}
	if string(f.Info) != "hello world" {
		t.Errorf("info: %q", f.Info)
	}
}

func TestAGWPortInfo(t *testing.T) {
	srv := NewServer(ServerConfig{
		PortCallsigns: []string{"N0CALL-1", "N0CALL-2"},
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv.cfg.ListenAddr = ln.Addr().String()
	_ = ln.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = srv.ListenAndServe(ctx) }()

	var conn net.Conn
	for i := 0; i < 50; i++ {
		c, err := net.Dial("tcp", srv.cfg.ListenAddr)
		if err == nil {
			conn = c
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if conn == nil {
		t.Fatal("no connect")
	}
	defer conn.Close()

	_ = WriteFrame(conn, &Header{DataKind: KindPortInfo}, nil)
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	h, data, err := ReadFrame(conn)
	if err != nil {
		t.Fatal(err)
	}
	if h.DataKind != KindPortInfo {
		t.Errorf("kind: %c", h.DataKind)
	}
	if len(data) == 0 || data[0] != '2' {
		t.Errorf("port count wrong: %q", data)
	}
}
