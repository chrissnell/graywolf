package modembridge

import (
	pb "github.com/chrissnell/graywolf/pkg/ipcproto"
	"testing"
)

func TestTransmitEncodingDispatch(t *testing.T) {
	var got *pb.TransmitEncoding
	b := New(Config{OnTransmitEncoding: func(m *pb.TransmitEncoding) { got = m }})
	expected := &pb.TransmitEncoding{FrameId: 42, Channel: 2, Encoding: "ax25", Fx25Requested: true, FallbackReason: "frame_too_large"}
	b.dispatchIPC(&pb.IpcMessage{Payload: &pb.IpcMessage_TransmitEncoding{TransmitEncoding: expected}})
	if got != expected {
		t.Fatal("native encoding report was not delivered")
	}
}
