//go:build android

package platformsvc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	pb "github.com/chrissnell/graywolf/pkg/platformproto"
)

// BondedBtDevice is the Go-side view of an Android Bluetooth bond entry
// returned by Client.BondedBtDevices. MAC is the colon-separated uppercase
// address (e.g. "AA:BB:CC:DD:EE:FF"); Name is the user-visible label set
// at bond time and may be empty if the device never advertised one.
type BondedBtDevice struct {
	MAC  string
	Name string
}

// SerialErrorErr is returned by btReadWriteCloser.Read when the platform
// service reports an out-of-band stream failure (e.g. "bond_lost",
// "rfcomm_closed"). Distinct from io.EOF (which signals normal close) so
// callers can decide whether to surface the cause to the operator.
type SerialErrorErr struct {
	Code   string
	Detail string
}

func (e *SerialErrorErr) Error() string {
	if e.Detail == "" {
		return fmt.Sprintf("platformsvc: serial error: %s", e.Code)
	}
	return fmt.Sprintf("platformsvc: serial error: %s: %s", e.Code, e.Detail)
}

// btSerialMaxChunk caps the payload size of a single SerialData frame.
// writeFrame enforces a 64 KiB hard limit (header + payload); chunking at
// 4 KiB keeps overhead low and matches typical KISS frame sizes.
const btSerialMaxChunk = 4 * 1024

// BondedBtDevices issues a one-shot request to the platform service for
// the current bonded-Bluetooth-device set. Order matches the platform's
// view at the moment of the request; subsequent bond/unbond events do not
// stream back here (use the platform-service's bond-state broadcast for
// live updates).
func (c *clientImpl) BondedBtDevices(ctx context.Context) ([]BondedBtDevice, error) {
	req := &pb.PlatformMessage{Body: &pb.PlatformMessage_BondedBtDevicesRequest{
		BondedBtDevicesRequest: &pb.BondedBtDevicesRequest{},
	}}
	resp, err := c.roundTrip(ctx, req)
	if err != nil {
		return nil, err
	}
	body := resp.GetBondedBtDevicesResponse()
	if body == nil {
		return nil, fmt.Errorf("platformsvc: expected BondedBtDevicesResponse, got %T", resp.GetBody())
	}
	out := make([]BondedBtDevice, 0, len(body.GetDevices()))
	for _, d := range body.GetDevices() {
		out = append(out, BondedBtDevice{MAC: d.GetMac(), Name: d.GetName()})
	}
	return out, nil
}

// BtSerialOpen opens an RFCOMM SPP stream to the bonded device at mac and
// returns an io.ReadWriteCloser. The returned handle multiplexes onto the
// shared UDS connection; multiple BtSerialOpen calls coexist, each routed
// by its 32-bit handle ID. The caller must Close when done so the platform
// service tears down the RFCOMM socket and releases the bond reference.
func (c *clientImpl) BtSerialOpen(ctx context.Context, mac string) (io.ReadWriteCloser, error) {
	if c.closed.Load() {
		return nil, ErrClosed
	}

	handle := c.nextBtHandle()
	inbound := c.registerBtHandle(handle)

	req := &pb.PlatformMessage{Body: &pb.PlatformMessage_SerialOpen{
		SerialOpen: &pb.SerialOpen{
			Handle:  handle,
			Kind:    pb.SerialKind_SERIAL_KIND_BLUETOOTH,
			Address: mac,
		},
	}}
	if err := c.send(req); err != nil {
		c.removeBtHandle(handle)
		return nil, fmt.Errorf("platformsvc: send SerialOpen: %w", err)
	}

	// Wait for SerialOpenAck (or an early SerialError on this handle).
	for {
		select {
		case <-ctx.Done():
			c.removeBtHandle(handle)
			// Best-effort: tell the server we're abandoning this handle
			// so it can tear down any in-progress connect attempt.
			_ = c.send(&pb.PlatformMessage{Body: &pb.PlatformMessage_SerialClose{
				SerialClose: &pb.SerialClose{Handle: handle, Reason: "client_cancel"},
			}})
			return nil, ctx.Err()
		case <-c.closeCh:
			c.removeBtHandle(handle)
			return nil, ErrClosed
		case msg, ok := <-inbound:
			if !ok {
				c.removeBtHandle(handle)
				return nil, ErrDisconnected
			}
			switch b := msg.GetBody().(type) {
			case *pb.PlatformMessage_SerialOpenAck:
				ack := b.SerialOpenAck
				if !ack.GetOk() {
					c.removeBtHandle(handle)
					return nil, fmt.Errorf("platformsvc: SerialOpen denied: %s", ack.GetError())
				}
				return newBtReadWriteCloser(c, handle, inbound), nil
			case *pb.PlatformMessage_SerialError:
				se := b.SerialError
				c.removeBtHandle(handle)
				return nil, &SerialErrorErr{Code: se.GetCode(), Detail: se.GetDetail()}
			case *pb.PlatformMessage_SerialClose:
				c.removeBtHandle(handle)
				return nil, fmt.Errorf("platformsvc: server closed before ack: %s", b.SerialClose.GetReason())
			default:
				// Unexpected payload for this handle before ack; ignore
				// and keep waiting. (Defensive: dispatch only routes the
				// four serial_* variants here, so this branch is dead
				// code today.)
			}
		}
	}
}

// btReadWriteCloser is a multiplexed RFCOMM stream over the platform-service
// UDS. Read blocks on the per-handle inbound channel; Write chunks the
// caller's bytes into ≤4 KiB SerialData frames; Close is idempotent and
// sends a final SerialClose to the server.
type btReadWriteCloser struct {
	c       *clientImpl
	handle  uint32
	inbound chan *pb.PlatformMessage

	// buf holds overflow bytes from a previous Read when the caller's
	// buffer was smaller than the incoming SerialData payload.
	bufMu sync.Mutex
	buf   []byte

	// closed fires once when Close is invoked, unblocking any in-flight
	// Read. closeOnce guards both the channel close and the SerialClose
	// emit so Close is safe to call from any goroutine.
	closed    chan struct{}
	closeOnce sync.Once
	closeErr  error
}

func newBtReadWriteCloser(c *clientImpl, handle uint32, inbound chan *pb.PlatformMessage) *btReadWriteCloser {
	return &btReadWriteCloser{
		c:       c,
		handle:  handle,
		inbound: inbound,
		closed:  make(chan struct{}),
	}
}

// Read implements io.Reader. Returns io.EOF when the server emits
// SerialClose, *SerialErrorErr on SerialError, and forwards SerialData
// bytes (stashing any tail that exceeds p's capacity).
func (r *btReadWriteCloser) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	// Drain any stashed overflow first.
	r.bufMu.Lock()
	if len(r.buf) > 0 {
		n := copy(p, r.buf)
		r.buf = r.buf[n:]
		if len(r.buf) == 0 {
			r.buf = nil
		}
		r.bufMu.Unlock()
		return n, nil
	}
	r.bufMu.Unlock()

	for {
		select {
		case <-r.closed:
			return 0, io.EOF
		case msg, ok := <-r.inbound:
			if !ok {
				return 0, io.EOF
			}
			switch b := msg.GetBody().(type) {
			case *pb.PlatformMessage_SerialData:
				data := b.SerialData.GetData()
				if len(data) == 0 {
					continue
				}
				n := copy(p, data)
				if n < len(data) {
					tail := make([]byte, len(data)-n)
					copy(tail, data[n:])
					r.bufMu.Lock()
					// Stash for the next Read. There's no concurrent
					// reader by contract; defensive lock guards against
					// misuse without changing semantics.
					r.buf = append(r.buf, tail...)
					r.bufMu.Unlock()
				}
				return n, nil
			case *pb.PlatformMessage_SerialClose:
				return 0, io.EOF
			case *pb.PlatformMessage_SerialError:
				se := b.SerialError
				return 0, &SerialErrorErr{Code: se.GetCode(), Detail: se.GetDetail()}
			case *pb.PlatformMessage_SerialOpenAck:
				// Spurious post-open ack — drop and keep reading.
				continue
			default:
				// Unknown payload routed to this handle; defensive drop.
				continue
			}
		}
	}
}

// Write implements io.Writer. Chunks p into ≤4 KiB SerialData frames and
// sends each in order. Returns the number of bytes successfully written
// (which always equals len(p) on a nil error return).
func (r *btReadWriteCloser) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	select {
	case <-r.closed:
		return 0, io.ErrClosedPipe
	default:
	}
	written := 0
	for written < len(p) {
		chunkEnd := written + btSerialMaxChunk
		if chunkEnd > len(p) {
			chunkEnd = len(p)
		}
		chunk := p[written:chunkEnd]
		msg := &pb.PlatformMessage{Body: &pb.PlatformMessage_SerialData{
			SerialData: &pb.SerialData{
				Handle: r.handle,
				Data:   append([]byte(nil), chunk...),
			},
		}}
		if err := r.c.send(msg); err != nil {
			if written > 0 {
				return written, err
			}
			return 0, err
		}
		written = chunkEnd
	}
	return written, nil
}

// Close releases the handle. Sends a final SerialClose to the server so
// the RFCOMM socket can be torn down and unregisters the per-handle
// channel. Safe to call multiple times; only the first invocation
// performs I/O. Returns the underlying send error from the first call
// (if any) for subsequent callers too.
func (r *btReadWriteCloser) Close() error {
	r.closeOnce.Do(func() {
		close(r.closed)
		msg := &pb.PlatformMessage{Body: &pb.PlatformMessage_SerialClose{
			SerialClose: &pb.SerialClose{Handle: r.handle, Reason: "client_close"},
		}}
		err := r.c.send(msg)
		// Suppress "not connected" / closed-client errors — Close on a
		// dead connection is not actionable to the caller.
		if err != nil && !errors.Is(err, ErrClosed) {
			r.closeErr = err
		}
		r.c.removeBtHandle(r.handle)
	})
	return r.closeErr
}
