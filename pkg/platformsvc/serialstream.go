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

// SerialErrorErr is returned by serialReadWriteCloser.Read when the platform
// service reports an out-of-band stream failure (e.g. "bond_lost",
// "usb_detached", "io_error"). Distinct from io.EOF (which signals normal
// close) so callers can decide whether to surface the cause to the operator.
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

// serialMaxChunk caps the payload size of a single SerialData frame.
// writeFrame enforces a 64 KiB hard limit (header + payload); chunking at
// 4 KiB keeps overhead low and matches typical KISS frame sizes.
const serialMaxChunk = 4 * 1024

// openSerialStream allocates a handle, sends a SerialOpen built by `build`,
// and blocks until the platform service replies SerialOpenAck (success),
// SerialError, SerialClose, or the context/closeCh fires. It is the shared
// open path for every serial transport (Bluetooth, USB); the only
// transport-specific input is the SerialOpen message `build` returns.
func (c *clientImpl) openSerialStream(ctx context.Context, build func(handle uint32) *pb.SerialOpen) (io.ReadWriteCloser, error) {
	if c.closed.Load() {
		return nil, ErrClosed
	}

	handle := c.nextSerialHandle()
	inbound := c.registerSerialHandle(handle)

	req := &pb.PlatformMessage{Body: &pb.PlatformMessage_SerialOpen{SerialOpen: build(handle)}}
	if err := c.send(req); err != nil {
		c.removeSerialHandle(handle)
		return nil, fmt.Errorf("platformsvc: send SerialOpen: %w", err)
	}

	for {
		select {
		case <-ctx.Done():
			c.removeSerialHandle(handle)
			// Best-effort: tell the server we're abandoning this handle so it
			// can tear down any in-progress open.
			_ = c.send(&pb.PlatformMessage{Body: &pb.PlatformMessage_SerialClose{
				SerialClose: &pb.SerialClose{Handle: handle, Reason: "client_cancel"},
			}})
			return nil, ctx.Err()
		case <-c.closeCh:
			c.removeSerialHandle(handle)
			return nil, ErrClosed
		case msg, ok := <-inbound:
			if !ok {
				c.removeSerialHandle(handle)
				return nil, ErrDisconnected
			}
			switch b := msg.GetBody().(type) {
			case *pb.PlatformMessage_SerialOpenAck:
				ack := b.SerialOpenAck
				if !ack.GetOk() {
					c.removeSerialHandle(handle)
					return nil, fmt.Errorf("platformsvc: SerialOpen denied: %s", ack.GetError())
				}
				return newSerialReadWriteCloser(c, handle, inbound), nil
			case *pb.PlatformMessage_SerialError:
				se := b.SerialError
				c.removeSerialHandle(handle)
				return nil, &SerialErrorErr{Code: se.GetCode(), Detail: se.GetDetail()}
			case *pb.PlatformMessage_SerialClose:
				c.removeSerialHandle(handle)
				return nil, fmt.Errorf("platformsvc: server closed before ack: %s", b.SerialClose.GetReason())
			default:
				// Unexpected payload before ack; keep waiting.
			}
		}
	}
}

// serialReadWriteCloser is a multiplexed byte stream over the platform-service
// UDS, transport-agnostic across Bluetooth RFCOMM and USB serial. Read blocks
// on the per-handle inbound channel; Write chunks the caller's bytes into
// <=4 KiB SerialData frames; Close is idempotent and sends a final SerialClose.
type serialReadWriteCloser struct {
	c       *clientImpl
	handle  uint32
	inbound chan *pb.PlatformMessage

	bufMu sync.Mutex
	buf   []byte

	closed    chan struct{}
	closeOnce sync.Once
	closeErr  error
}

func newSerialReadWriteCloser(c *clientImpl, handle uint32, inbound chan *pb.PlatformMessage) *serialReadWriteCloser {
	return &serialReadWriteCloser{
		c:       c,
		handle:  handle,
		inbound: inbound,
		closed:  make(chan struct{}),
	}
}

// Read implements io.Reader. Returns io.EOF when the server emits
// SerialClose, *SerialErrorErr on SerialError, and forwards SerialData
// bytes (stashing any tail that exceeds p's capacity).
func (r *serialReadWriteCloser) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

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
					r.bufMu.Lock()
					r.buf = append(r.buf, data[n:]...)
					r.bufMu.Unlock()
				}
				return n, nil
			case *pb.PlatformMessage_SerialClose:
				return 0, io.EOF
			case *pb.PlatformMessage_SerialError:
				se := b.SerialError
				return 0, &SerialErrorErr{Code: se.GetCode(), Detail: se.GetDetail()}
			case *pb.PlatformMessage_SerialOpenAck:
				continue
			default:
				continue
			}
		}
	}
}

// Write implements io.Writer. Chunks p into <=4 KiB SerialData frames.
func (r *serialReadWriteCloser) Write(p []byte) (int, error) {
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
		chunkEnd := min(written+serialMaxChunk, len(p))
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

// Close releases the handle. Sends a final SerialClose to the server and
// unregisters the per-handle channel. Idempotent; only the first invocation
// performs I/O.
func (r *serialReadWriteCloser) Close() error {
	r.closeOnce.Do(func() {
		close(r.closed)
		msg := &pb.PlatformMessage{Body: &pb.PlatformMessage_SerialClose{
			SerialClose: &pb.SerialClose{Handle: r.handle, Reason: "client_close"},
		}}
		err := r.c.send(msg)
		if err != nil && !errors.Is(err, ErrClosed) {
			r.closeErr = err
		}
		r.c.removeSerialHandle(r.handle)
	})
	return r.closeErr
}
