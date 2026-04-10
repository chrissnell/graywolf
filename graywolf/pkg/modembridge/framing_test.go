package modembridge

import (
	"bytes"
	"io"
	"testing"

	pb "github.com/chrissnell/graywolf/pkg/ipcproto"
)

func TestFrameRoundTrip(t *testing.T) {
	orig := &pb.IpcMessage{Payload: &pb.IpcMessage_ModemReady{
		ModemReady: &pb.ModemReady{Version: "test", Pid: 42},
	}}
	var buf bytes.Buffer
	if err := writeFrame(&buf, orig); err != nil {
		t.Fatalf("writeFrame: %v", err)
	}
	got, err := readFrame(&buf)
	if err != nil {
		t.Fatalf("readFrame: %v", err)
	}
	mr := got.GetModemReady()
	if mr == nil || mr.Version != "test" || mr.Pid != 42 {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}

func TestReadFrameEOF(t *testing.T) {
	var buf bytes.Buffer
	if _, err := readFrame(&buf); err != io.EOF {
		t.Fatalf("expected EOF, got %v", err)
	}
}

func TestMultipleFrames(t *testing.T) {
	msgs := []*pb.IpcMessage{
		{Payload: &pb.IpcMessage_ModemReady{ModemReady: &pb.ModemReady{Version: "a"}}},
		{Payload: &pb.IpcMessage_StatusUpdate{StatusUpdate: &pb.StatusUpdate{Channel: 1, RxFrames: 3}}},
	}
	var buf bytes.Buffer
	for _, m := range msgs {
		if err := writeFrame(&buf, m); err != nil {
			t.Fatal(err)
		}
	}
	for i := range msgs {
		got, err := readFrame(&buf)
		if err != nil {
			t.Fatalf("frame %d: %v", i, err)
		}
		_ = got
	}
	if _, err := readFrame(&buf); err != io.EOF {
		t.Fatalf("expected EOF after all frames, got %v", err)
	}
}
