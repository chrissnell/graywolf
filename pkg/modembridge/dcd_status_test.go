package modembridge

import (
	pb "github.com/chrissnell/graywolf/pkg/ipcproto"
	"testing"
)

// A quiet demodulator after reconfiguration may emit no fresh DCD edge.
// Its periodic authoritative status must repair the previous busy state
// delivered to the TX governor, without overriding a genuinely busy status.
func TestStatusReconcilesDCDWithoutNewCarrierEdge(t *testing.T) {
	b := New(Config{})
	events := b.DcdEvents()
	b.dispatchIPC(&pb.IpcMessage{Payload: &pb.IpcMessage_DcdChange{DcdChange: &pb.DcdChange{Channel: 2, Detected: true}}})
	if !((<-events).Detected) {
		t.Fatal("expected initial busy edge")
	}
	// Placeholder/legacy status is not evidence that a busy channel cleared.
	b.dispatchIPC(&pb.IpcMessage{Payload: &pb.IpcMessage_StatusUpdate{StatusUpdate: &pb.StatusUpdate{Channel: 2, DcdState: false}}})
	select {
	case <-events:
		t.Fatal("placeholder DCD cleared carrier sense")
	default:
	}
	b.dispatchIPC(&pb.IpcMessage{Payload: &pb.IpcMessage_StatusUpdate{StatusUpdate: &pb.StatusUpdate{Channel: 2, DcdState: false, DcdStateValid: true}}})
	select {
	case e := <-events:
		if e.Channel != 2 || e.Detected {
			t.Fatalf("expected quiet channel 2, got %+v", e)
		}
	default:
		t.Fatal("quiet status did not clear stale carrier-busy state")
	}
	b.dispatchIPC(&pb.IpcMessage{Payload: &pb.IpcMessage_StatusUpdate{StatusUpdate: &pb.StatusUpdate{Channel: 2, DcdState: false, DcdStateValid: true}}})
	select {
	case e := <-events:
		t.Fatalf("redundant quiet edge: %+v", e)
	default:
	}
	b.dispatchIPC(&pb.IpcMessage{Payload: &pb.IpcMessage_StatusUpdate{StatusUpdate: &pb.StatusUpdate{Channel: 2, DcdState: true, DcdStateValid: true}}})
	select {
	case e := <-events:
		if !e.Detected {
			t.Fatal("cleared genuine busy carrier")
		}
	default:
		t.Fatal("busy status not reconciled")
	}
}
