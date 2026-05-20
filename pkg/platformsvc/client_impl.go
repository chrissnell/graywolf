//go:build android

package platformsvc

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	pb "github.com/chrissnell/graywolf/pkg/platformproto"
)

// Version of the client; injected at build time. Falls back to a sentinel.
var clientBuildVersion = "v0.0.0-dev"

type clientImpl struct {
	socketPath string

	mu      sync.Mutex
	conn    net.Conn
	closed  atomic.Bool
	closeCh chan struct{}

	subsMu         sync.Mutex
	gpsFixSubs     []chan<- *GpsFix
	gnssStatusSubs []chan<- *GnssStatusUpdate
	audioRouteSubs []chan<- *AudioRouteChanged

	// Single in-flight request → response correlation. The platform proto
	// has no request_id field, so we serialize requests through requestMu.
	requestMu sync.Mutex
	respCh    chan *pb.PlatformMessage // re-set per request
}

func newClient(socketPath string) Client {
	return &clientImpl{
		socketPath: socketPath,
		closeCh:    make(chan struct{}),
	}
}

// ConnectWithReconnect dials the UDS and, on disconnect, re-dials with
// exponential backoff (backoffSchedule). The reconnect loop runs until
// ctx is cancelled or Close() is called. The first dial is synchronous;
// subsequent dials happen in a background goroutine.
//
// This is the production entry point exposed via the Client interface.
// The internal one-shot Connect path stays for tests only.
func (c *clientImpl) ConnectWithReconnect(ctx context.Context) error {
	if err := c.Connect(ctx); err != nil {
		return err
	}
	go c.reconnectLoop(ctx)
	return nil
}

func (c *clientImpl) reconnectLoop(ctx context.Context) {
	for {
		// Wait until current conn drops.
		c.mu.Lock()
		conn := c.conn
		c.mu.Unlock()
		if conn == nil {
			// Already disconnected; back off and retry.
		} else {
			select {
			case <-c.closeCh:
				return
			case <-ctx.Done():
				return
			case <-time.After(50 * time.Millisecond):
				continue
			}
		}

		var lastErr error
		for _, delay := range backoffSchedule {
			select {
			case <-c.closeCh:
				return
			case <-ctx.Done():
				return
			case <-time.After(delay):
			}
			if err := c.Connect(ctx); err == nil {
				lastErr = nil
				break
			} else {
				lastErr = err
			}
		}
		_ = lastErr
	}
}

// injectConn is only used by tests; replaces the live UDS with a net.Pipe
// half. Starts the read loop synchronously.
func (c *clientImpl) injectConn(conn net.Conn) {
	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()
	go c.readLoop(conn)
}

func (c *clientImpl) Connect(ctx context.Context) error {
	if c.socketPath == "" {
		return errors.New("platformsvc: empty socket path")
	}
	d := net.Dialer{}
	conn, err := d.DialContext(ctx, "unix", c.socketPath)
	if err != nil {
		return fmt.Errorf("platformsvc: dial: %w", err)
	}
	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()
	go c.readLoop(conn)
	return nil
}

func (c *clientImpl) readLoop(conn net.Conn) {
	for {
		msg, err := readFrame(conn)
		if err != nil {
			c.handleDisconnect(err)
			return
		}
		c.dispatch(msg)
	}
}

func (c *clientImpl) dispatch(msg *pb.PlatformMessage) {
	switch b := msg.GetBody().(type) {
	case *pb.PlatformMessage_GpsFix:
		c.subsMu.Lock()
		subs := append([]chan<- *GpsFix{}, c.gpsFixSubs...)
		c.subsMu.Unlock()
		for _, s := range subs {
			select {
			case s <- b.GpsFix:
			default:
			}
		}
	case *pb.PlatformMessage_AudioRouteChanged:
		c.subsMu.Lock()
		subs := append([]chan<- *AudioRouteChanged{}, c.audioRouteSubs...)
		c.subsMu.Unlock()
		for _, s := range subs {
			select {
			case s <- b.AudioRouteChanged:
			default:
			}
		}
	case *pb.PlatformMessage_GnssStatus:
		c.subsMu.Lock()
		subs := append([]chan<- *GnssStatusUpdate{}, c.gnssStatusSubs...)
		c.subsMu.Unlock()
		for _, s := range subs {
			select {
			case s <- b.GnssStatus:
			default:
			}
		}
	default:
		// Response to an in-flight request. We only forward when the
		// oneof body matches a request type we actually issue from
		// roundTrip; any other unsolicited message gets logged and
		// dropped so it can't be misattributed to the in-flight
		// caller. (No request_id field on the wire; correlation is
		// strictly by ordering + oneof shape under requestMu.)
		switch msg.GetBody().(type) {
		case *pb.PlatformMessage_Hello,
			*pb.PlatformMessage_UsbListResp,
			*pb.PlatformMessage_UsbSelectResp,
			*pb.PlatformMessage_PttAck,
			*pb.PlatformMessage_AudioListResp,
			*pb.PlatformMessage_BondedBtDevicesResponse,
			*pb.PlatformMessage_Error:
			c.mu.Lock()
			ch := c.respCh
			c.mu.Unlock()
			if ch != nil {
				select {
				case ch <- msg:
				default:
				}
			}
		default:
			// Unsolicited push of a request-type or unknown variant —
			// silently drop. Phase 4+ may add Logf here.
		}
	}
}

// handleDisconnect is called from readLoop when the underlying conn dies.
// It closes the in-flight response channel (if any) so a caller blocked in
// roundTrip's select wakes up immediately rather than deadlocking until
// ctx expires. The reconnect loop sees c.conn == nil and re-dials.
func (c *clientImpl) handleDisconnect(_ error) {
	c.mu.Lock()
	c.conn = nil
	respCh := c.respCh
	c.respCh = nil
	c.mu.Unlock()
	if respCh != nil {
		// Closing surfaces as `_, ok := <-respCh; !ok` in roundTrip.
		close(respCh)
	}
}

func (c *clientImpl) Close() error {
	if c.closed.Swap(true) {
		return nil
	}
	close(c.closeCh)
	c.mu.Lock()
	conn := c.conn
	c.conn = nil
	c.mu.Unlock()
	if conn != nil {
		return conn.Close()
	}
	return nil
}

// roundTrip serializes one request, awaits exactly one response.
func (c *clientImpl) roundTrip(ctx context.Context, req *pb.PlatformMessage) (*pb.PlatformMessage, error) {
	if c.closed.Load() {
		return nil, ErrClosed
	}
	c.requestMu.Lock()
	defer c.requestMu.Unlock()

	respCh := make(chan *pb.PlatformMessage, 1)
	c.mu.Lock()
	conn := c.conn
	c.respCh = respCh
	c.mu.Unlock()
	if conn == nil {
		return nil, errors.New("platformsvc: not connected")
	}

	if err := writeFrame(conn, req); err != nil {
		return nil, fmt.Errorf("write: %w", err)
	}
	select {
	case resp, ok := <-respCh:
		if !ok {
			// handleDisconnect closed the channel — conn died.
			return nil, ErrDisconnected
		}
		if e := resp.GetError(); e != nil {
			return nil, &ErrServerError{Code: e.Code, Message: e.Message}
		}
		return resp, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.closeCh:
		return nil, ErrClosed
	}
}

func (c *clientImpl) Hello(ctx context.Context, schemaVersion uint32) (*HelloResponse, error) {
	req := &pb.PlatformMessage{Body: &pb.PlatformMessage_Hello{
		Hello: &pb.Hello{
			SchemaVersion: schemaVersion,
			ClientVersion: clientBuildVersion,
		},
	}}
	resp, err := c.roundTrip(ctx, req)
	if err != nil {
		return nil, err
	}
	hello := resp.GetHello()
	if hello == nil {
		return nil, fmt.Errorf("platformsvc: expected Hello response, got %T", resp.GetBody())
	}
	if hello.SchemaVersion != schemaVersion {
		// Spec §1.3: terminate the client. Closing here also tears down
		// the reconnect loop so we don't keep redialing into the same
		// mismatch.
		_ = c.Close()
		return nil, &ErrSchemaMismatch{ClientVersion: schemaVersion, ServerVersion: hello.SchemaVersion}
	}
	return hello, nil
}

func (c *clientImpl) ListUsbDevices(ctx context.Context, class UsbClass) ([]*UsbDevice, error) {
	req := &pb.PlatformMessage{Body: &pb.PlatformMessage_UsbListReq{
		UsbListReq: &pb.UsbDeviceListRequest{ClassFilter: class},
	}}
	resp, err := c.roundTrip(ctx, req)
	if err != nil {
		return nil, err
	}
	if r := resp.GetUsbListResp(); r != nil {
		return r.Devices, nil
	}
	return nil, fmt.Errorf("platformsvc: unexpected response %T", resp.GetBody())
}

func (c *clientImpl) SelectUsbDevice(ctx context.Context, vid, pid uint16) (*UsbHandle, error) {
	req := &pb.PlatformMessage{Body: &pb.PlatformMessage_UsbSelectReq{
		UsbSelectReq: &pb.UsbSelectRequest{Vid: uint32(vid), Pid: uint32(pid)},
	}}
	resp, err := c.roundTrip(ctx, req)
	if err != nil {
		return nil, err
	}
	r := resp.GetUsbSelectResp()
	if r == nil {
		return nil, fmt.Errorf("platformsvc: unexpected response %T", resp.GetBody())
	}
	if !r.Granted {
		return nil, fmt.Errorf("platformsvc: usb select denied: %s", r.ErrorMessage)
	}
	return &UsbHandle{ID: r.HandleId, Vid: vid, Pid: pid}, nil
}

func (c *clientImpl) KeyPtt(ctx context.Context, method PttMethod, handle *UsbHandle) (*PttAck, error) {
	hid := ""
	if handle != nil {
		hid = handle.ID
	}
	req := &pb.PlatformMessage{Body: &pb.PlatformMessage_PttKeyReq{
		PttKeyReq: &pb.PttKeyRequest{Method: method, HandleId: hid},
	}}
	resp, err := c.roundTrip(ctx, req)
	if err != nil {
		return nil, err
	}
	if a := resp.GetPttAck(); a != nil {
		return a, nil
	}
	return nil, fmt.Errorf("platformsvc: unexpected response %T", resp.GetBody())
}

func (c *clientImpl) UnkeyPtt(ctx context.Context, method PttMethod, handle *UsbHandle) (*PttAck, error) {
	hid := ""
	if handle != nil {
		hid = handle.ID
	}
	req := &pb.PlatformMessage{Body: &pb.PlatformMessage_PttUnkeyReq{
		PttUnkeyReq: &pb.PttUnkeyRequest{Method: method, HandleId: hid},
	}}
	resp, err := c.roundTrip(ctx, req)
	if err != nil {
		return nil, err
	}
	if a := resp.GetPttAck(); a != nil {
		return a, nil
	}
	return nil, fmt.Errorf("platformsvc: unexpected response %T", resp.GetBody())
}

// Used in reconnect_test.go to assert backoff behaviour.
var backoffSchedule = []time.Duration{
	100 * time.Millisecond, 200 * time.Millisecond,
	400 * time.Millisecond, 800 * time.Millisecond,
	1600 * time.Millisecond, 5 * time.Second,
}
