//go:build android

package platformsvc

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	pb "github.com/chrissnell/graywolf/pkg/platformproto"
)

func TestBondedBtDevices_returnsDevices(t *testing.T) {
	respond := func(req *pb.PlatformMessage) *pb.PlatformMessage {
		if req.GetBondedBtDevicesRequest() == nil {
			return nil
		}
		return &pb.PlatformMessage{Body: &pb.PlatformMessage_BondedBtDevicesResponse{
			BondedBtDevicesResponse: &pb.BondedBtDevicesResponse{
				Devices: []*pb.BondedBtDevicesResponse_Device{
					{Mac: "AA:BB:CC:DD:EE:FF", Name: "Mobilinkd TNC4"},
					{Mac: "11:22:33:44:55:66", Name: ""},
				},
			},
		}}
	}
	withFakeClient(t, respond, func(c *clientImpl) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		got, err := c.BondedBtDevices(ctx)
		if err != nil {
			t.Fatalf("BondedBtDevices: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("expected 2 devices, got %d", len(got))
		}
		if got[0].MAC != "AA:BB:CC:DD:EE:FF" || got[0].Name != "Mobilinkd TNC4" {
			t.Errorf("device[0] = %+v", got[0])
		}
		if got[1].MAC != "11:22:33:44:55:66" || got[1].Name != "" {
			t.Errorf("device[1] = %+v", got[1])
		}
	})
}

// btTestServer is a per-test driver for BtSerialOpen: it owns the server
// half of the pipe, reads incoming frames in a goroutine, and lets the
// test push response frames at will.
type btTestServer struct {
	t       *testing.T
	conn    net.Conn
	inMu    sync.Mutex
	inbound []*pb.PlatformMessage
	in      chan *pb.PlatformMessage
	wg      sync.WaitGroup
}

func newBtTestServer(t *testing.T) (*btTestServer, *clientImpl) {
	t.Helper()
	clientConn, serverConn := net.Pipe()
	srv := &btTestServer{
		t:    t,
		conn: serverConn,
		in:   make(chan *pb.PlatformMessage, 16),
	}
	srv.wg.Add(1)
	go srv.readLoop()
	c := newClient("").(*clientImpl)
	c.injectConn(clientConn)
	return srv, c
}

func (s *btTestServer) readLoop() {
	defer s.wg.Done()
	for {
		msg, err := readFrame(s.conn)
		if err != nil {
			return
		}
		s.inMu.Lock()
		s.inbound = append(s.inbound, msg)
		s.inMu.Unlock()
		select {
		case s.in <- msg:
		default:
		}
	}
}

func (s *btTestServer) waitFor(t *testing.T, match func(*pb.PlatformMessage) bool, timeout time.Duration) *pb.PlatformMessage {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case msg := <-s.in:
			if match(msg) {
				return msg
			}
		case <-deadline:
			t.Fatal("timed out waiting for matching frame")
			return nil
		}
	}
}

func (s *btTestServer) send(t *testing.T, msg *pb.PlatformMessage) {
	t.Helper()
	if err := writeFrame(s.conn, msg); err != nil {
		t.Fatalf("server send: %v", err)
	}
}

func (s *btTestServer) close() {
	s.conn.Close()
	s.wg.Wait()
}

func TestBtSerialOpen_roundTrip(t *testing.T) {
	srv, c := newBtTestServer(t)
	defer srv.close()

	// Drive BtSerialOpen + a write + a read in a goroutine; let the
	// server respond inline so we don't deadlock on net.Pipe.
	openDone := make(chan error, 1)
	rwcCh := make(chan io.ReadWriteCloser, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		rwc, err := c.BtSerialOpen(ctx, "AA:BB:CC:DD:EE:FF")
		if err != nil {
			openDone <- err
			return
		}
		rwcCh <- rwc
		openDone <- nil
	}()

	// Expect SerialOpen, ack it.
	open := srv.waitFor(t, func(m *pb.PlatformMessage) bool {
		return m.GetSerialOpen() != nil
	}, 2*time.Second)
	if got, want := open.GetSerialOpen().GetAddress(), "AA:BB:CC:DD:EE:FF"; got != want {
		t.Fatalf("SerialOpen address: got %q want %q", got, want)
	}
	if open.GetSerialOpen().GetKind() != pb.SerialKind_SERIAL_KIND_BLUETOOTH {
		t.Fatalf("SerialOpen kind: got %v", open.GetSerialOpen().GetKind())
	}
	handle := open.GetSerialOpen().GetHandle()
	srv.send(t, &pb.PlatformMessage{Body: &pb.PlatformMessage_SerialOpenAck{
		SerialOpenAck: &pb.SerialOpenAck{Handle: handle, Ok: true},
	}})

	if err := <-openDone; err != nil {
		t.Fatalf("BtSerialOpen: %v", err)
	}
	rwc := <-rwcCh
	defer rwc.Close()

	// Write some bytes, expect them as SerialData frames on the server.
	writeDone := make(chan error, 1)
	go func() {
		_, err := rwc.Write([]byte("KISS"))
		writeDone <- err
	}()
	data := srv.waitFor(t, func(m *pb.PlatformMessage) bool {
		return m.GetSerialData() != nil
	}, 2*time.Second)
	if !bytes.Equal(data.GetSerialData().GetData(), []byte("KISS")) {
		t.Fatalf("client wrote %q", data.GetSerialData().GetData())
	}
	if got := data.GetSerialData().GetHandle(); got != handle {
		t.Fatalf("handle mismatch: got %d want %d", got, handle)
	}
	if err := <-writeDone; err != nil {
		t.Fatalf("Write: %v", err)
	}

	// Server echoes data; client Read should surface it.
	srv.send(t, &pb.PlatformMessage{Body: &pb.PlatformMessage_SerialData{
		SerialData: &pb.SerialData{Handle: handle, Data: []byte("ACK!")},
	}})
	buf := make([]byte, 16)
	n, err := rwc.Read(buf)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !bytes.Equal(buf[:n], []byte("ACK!")) {
		t.Fatalf("Read got %q", buf[:n])
	}
}

func TestBtSerialOpen_serverClose_returnsEOF(t *testing.T) {
	srv, c := newBtTestServer(t)
	defer srv.close()

	openDone := make(chan io.ReadWriteCloser, 1)
	openErr := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		rwc, err := c.BtSerialOpen(ctx, "AA:BB:CC:DD:EE:FF")
		if err != nil {
			openErr <- err
			return
		}
		openDone <- rwc
	}()

	open := srv.waitFor(t, func(m *pb.PlatformMessage) bool {
		return m.GetSerialOpen() != nil
	}, 2*time.Second)
	handle := open.GetSerialOpen().GetHandle()
	srv.send(t, &pb.PlatformMessage{Body: &pb.PlatformMessage_SerialOpenAck{
		SerialOpenAck: &pb.SerialOpenAck{Handle: handle, Ok: true},
	}})

	var rwc io.ReadWriteCloser
	select {
	case rwc = <-openDone:
	case err := <-openErr:
		t.Fatalf("BtSerialOpen: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("BtSerialOpen did not return")
	}
	defer rwc.Close()

	// Server sends SerialClose on this handle; Read must return io.EOF.
	srv.send(t, &pb.PlatformMessage{Body: &pb.PlatformMessage_SerialClose{
		SerialClose: &pb.SerialClose{Handle: handle, Reason: "rfcomm_closed"},
	}})

	buf := make([]byte, 16)
	n, err := rwc.Read(buf)
	if err != io.EOF {
		t.Fatalf("Read err: got %v (%d bytes) want io.EOF", err, n)
	}
}

func TestBtSerialOpen_serialError_returnsTypedError(t *testing.T) {
	srv, c := newBtTestServer(t)
	defer srv.close()

	openDone := make(chan io.ReadWriteCloser, 1)
	openErr := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		rwc, err := c.BtSerialOpen(ctx, "AA:BB:CC:DD:EE:FF")
		if err != nil {
			openErr <- err
			return
		}
		openDone <- rwc
	}()

	open := srv.waitFor(t, func(m *pb.PlatformMessage) bool {
		return m.GetSerialOpen() != nil
	}, 2*time.Second)
	handle := open.GetSerialOpen().GetHandle()
	srv.send(t, &pb.PlatformMessage{Body: &pb.PlatformMessage_SerialOpenAck{
		SerialOpenAck: &pb.SerialOpenAck{Handle: handle, Ok: true},
	}})

	var rwc io.ReadWriteCloser
	select {
	case rwc = <-openDone:
	case err := <-openErr:
		t.Fatalf("BtSerialOpen: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("BtSerialOpen did not return")
	}
	defer rwc.Close()

	srv.send(t, &pb.PlatformMessage{Body: &pb.PlatformMessage_SerialError{
		SerialError: &pb.SerialError{
			Handle: handle,
			Code:   "bond_lost",
			Detail: "remote unbonded",
		},
	}})

	buf := make([]byte, 16)
	_, err := rwc.Read(buf)
	var serr *SerialErrorErr
	if !errors.As(err, &serr) {
		t.Fatalf("Read err: got %T %v, want *SerialErrorErr", err, err)
	}
	if serr.Code != "bond_lost" {
		t.Errorf("SerialErrorErr.Code = %q want %q", serr.Code, "bond_lost")
	}
	if serr.Detail != "remote unbonded" {
		t.Errorf("SerialErrorErr.Detail = %q", serr.Detail)
	}
}

// TestBtSerialOpen_clientClose_unblocksRead proves that calling the
// underlying client's Close() wakes a blocked serialReadWriteCloser.Read
// with io.EOF instead of hanging on the now-orphaned per-handle channel.
// Without drainSerialHandles in Close(), this test would deadlock until the
// test timeout. The 500 ms deadline is generous; the wakeup is effectively
// immediate once the channel is closed.
func TestBtSerialOpen_clientClose_unblocksRead(t *testing.T) {
	srv, c := newBtTestServer(t)
	defer srv.close()

	openDone := make(chan io.ReadWriteCloser, 1)
	openErr := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		rwc, err := c.BtSerialOpen(ctx, "AA:BB:CC:DD:EE:FF")
		if err != nil {
			openErr <- err
			return
		}
		openDone <- rwc
	}()

	open := srv.waitFor(t, func(m *pb.PlatformMessage) bool {
		return m.GetSerialOpen() != nil
	}, 2*time.Second)
	handle := open.GetSerialOpen().GetHandle()
	srv.send(t, &pb.PlatformMessage{Body: &pb.PlatformMessage_SerialOpenAck{
		SerialOpenAck: &pb.SerialOpenAck{Handle: handle, Ok: true},
	}})

	var rwc io.ReadWriteCloser
	select {
	case rwc = <-openDone:
	case err := <-openErr:
		t.Fatalf("BtSerialOpen: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("BtSerialOpen did not return")
	}

	// Kick off a blocked Read; nothing on the wire so it parks on the
	// inbound channel.
	type readResult struct {
		n   int
		err error
	}
	readCh := make(chan readResult, 1)
	go func() {
		buf := make([]byte, 16)
		n, err := rwc.Read(buf)
		readCh <- readResult{n, err}
	}()

	// Give the reader a beat to actually block.
	time.Sleep(20 * time.Millisecond)

	// Closing the client drains serialHandles -> Read sees channel close ->
	// returns io.EOF.
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	select {
	case res := <-readCh:
		if res.err != io.EOF {
			t.Fatalf("Read err after client Close: got %v want io.EOF", res.err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Read did not return within 500ms of client Close; drainSerialHandles missing?")
	}
}

// TestBtSerialOpen_serverDisconnect_unblocksRead proves the same property
// for handleDisconnect: when the UDS dies (server-side conn.Close), the
// client's drainSerialHandles call from handleDisconnect must wake any blocked
// per-handle Read with io.EOF.
func TestBtSerialOpen_serverDisconnect_unblocksRead(t *testing.T) {
	srv, c := newBtTestServer(t)
	// Do not defer srv.close() — we close the server conn explicitly below
	// to exercise the handleDisconnect path. Final cleanup of wg is via
	// readLoop returning on the broken pipe.
	defer c.Close()

	openDone := make(chan io.ReadWriteCloser, 1)
	openErr := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		rwc, err := c.BtSerialOpen(ctx, "AA:BB:CC:DD:EE:FF")
		if err != nil {
			openErr <- err
			return
		}
		openDone <- rwc
	}()

	open := srv.waitFor(t, func(m *pb.PlatformMessage) bool {
		return m.GetSerialOpen() != nil
	}, 2*time.Second)
	handle := open.GetSerialOpen().GetHandle()
	srv.send(t, &pb.PlatformMessage{Body: &pb.PlatformMessage_SerialOpenAck{
		SerialOpenAck: &pb.SerialOpenAck{Handle: handle, Ok: true},
	}})

	var rwc io.ReadWriteCloser
	select {
	case rwc = <-openDone:
	case err := <-openErr:
		t.Fatalf("BtSerialOpen: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("BtSerialOpen did not return")
	}
	defer rwc.Close()

	type readResult struct {
		n   int
		err error
	}
	readCh := make(chan readResult, 1)
	go func() {
		buf := make([]byte, 16)
		n, err := rwc.Read(buf)
		readCh <- readResult{n, err}
	}()

	time.Sleep(20 * time.Millisecond)

	// Closing the server side of the pipe causes the client's readLoop to
	// hit an error and invoke handleDisconnect, which drains serialHandles.
	srv.conn.Close()
	srv.wg.Wait()

	select {
	case res := <-readCh:
		if res.err != io.EOF {
			t.Fatalf("Read err after server disconnect: got %v want io.EOF", res.err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Read did not return within 500ms of server disconnect; drainSerialHandles missing in handleDisconnect?")
	}
}
