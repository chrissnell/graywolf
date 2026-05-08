package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"google.golang.org/protobuf/proto"

	pb "github.com/chrissnell/graywolf/pkg/ipcproto"
)

const maxFrameSize = 64 * 1024

func writeFrame(w io.Writer, msg *pb.IpcMessage) error {
	buf, err := proto.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal ipc: %w", err)
	}
	if len(buf) > maxFrameSize {
		return fmt.Errorf("frame too large: %d", len(buf))
	}
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(buf)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	_, err = w.Write(buf)
	return err
}

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
		return nil, fmt.Errorf("frame too large: %d", n)
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	var msg pb.IpcMessage
	if err := proto.Unmarshal(body, &msg); err != nil {
		return nil, fmt.Errorf("unmarshal ipc: %w", err)
	}
	return &msg, nil
}
