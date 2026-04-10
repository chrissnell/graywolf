package modembridge

import (
	"testing"
	"time"

	pb "github.com/chrissnell/graywolf/pkg/ipcproto"
)

func TestDcdFanOutToMultipleSubscribers(t *testing.T) {
	// Construct a Bridge without starting it; we exercise dispatchDcd
	// directly so no modem child is needed.
	b := New(Config{})
	subA := b.DcdSubscribe()
	subB := b.DcdSubscribe()

	for i := 0; i < 5; i++ {
		b.dispatchDcd(&pb.DcdChange{Channel: uint32(i), Detected: i%2 == 0})
	}

	drain := func(ch <-chan *pb.DcdChange, n int) int {
		got := 0
		deadline := time.After(200 * time.Millisecond)
		for got < n {
			select {
			case <-ch:
				got++
			case <-deadline:
				return got
			}
		}
		return got
	}
	if n := drain(subA, 5); n != 5 {
		t.Fatalf("subA got %d events want 5", n)
	}
	if n := drain(subB, 5); n != 5 {
		t.Fatalf("subB got %d events want 5", n)
	}
}

func TestRxHookInvoked(t *testing.T) {
	b := New(Config{})
	var count int
	b.SetRxHook(func(rf *pb.ReceivedFrame) { count++ })
	b.dispatchRx(&pb.ReceivedFrame{Channel: 1})
	b.dispatchRx(&pb.ReceivedFrame{Channel: 2})
	if count != 2 {
		t.Fatalf("rx hook called %d times, want 2", count)
	}
}
