package modembridge

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"google.golang.org/protobuf/proto"

	pb "github.com/chrissnell/graywolf/pkg/ipcproto"
)

// Matches src/ipc/framing.rs: [4-byte big-endian length][IpcMessage bytes].
// The Rust side caps frames at 64 KiB.
const maxFrameSize = 64 * 1024

// writeFrame serialises msg and writes it with the length prefix.
func writeFrame(w io.Writer, msg *pb.IpcMessage) error {
	buf, err := proto.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal ipc message: %w", err)
	}
	if len(buf) > maxFrameSize {
		return fmt.Errorf("frame too large: %d > %d", len(buf), maxFrameSize)
	}
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(buf)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	if _, err := w.Write(buf); err != nil {
		return err
	}
	return nil
}

// readFrame reads exactly one framed message. Returns io.EOF when the peer
// closes cleanly between frames.
func readFrame(r io.Reader) (*pb.IpcMessage, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		if errors.Is(err, io.ErrUnexpectedEOF) {
			return nil, io.EOF
		}
		return nil, err
	}
	n := binary.BigEndian.Uint32(hdr[:])
	if n > maxFrameSize {
		return nil, fmt.Errorf("frame too large: %d > %d", n, maxFrameSize)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	msg := &pb.IpcMessage{}
	if err := proto.Unmarshal(buf, msg); err != nil {
		return nil, fmt.Errorf("unmarshal ipc message: %w", err)
	}
	return msg, nil
}
