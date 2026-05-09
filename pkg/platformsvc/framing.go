//go:build android

package platformsvc

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"google.golang.org/protobuf/proto"

	pb "github.com/chrissnell/graywolf/pkg/platformproto"
)

// Matches pkg/modembridge/framing.go: [4 BE bytes length][bytes].
const maxFrameSize = 64 * 1024

func writeFrame(w io.Writer, msg *pb.PlatformMessage) error {
	buf, err := proto.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal platform message: %w", err)
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

func readFrame(r io.Reader) (*pb.PlatformMessage, error) {
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
	msg := &pb.PlatformMessage{}
	if err := proto.Unmarshal(buf, msg); err != nil {
		return nil, fmt.Errorf("unmarshal platform message: %w", err)
	}
	return msg, nil
}
