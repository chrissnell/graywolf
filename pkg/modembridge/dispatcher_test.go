package modembridge

import (
	"sync"
	"testing"
	"time"
)

type fakeResp struct{ value int }

// TestDispatcherRegisterDeliver verifies the happy path: Register produces
// a channel that sees the delivered response.
func TestDispatcherRegisterDeliver(t *testing.T) {
	d := newDispatcher[*fakeResp]()
	id, ch := d.Register()
	if id == 0 {
		t.Fatal("expected non-zero request id")
	}
	if !d.Deliver(id, &fakeResp{value: 42}) {
		t.Fatal("Deliver returned false on registered id")
	}
	select {
	case got := <-ch:
		if got == nil || got.value != 42 {
			t.Fatalf("got %+v, want {42}", got)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("channel did not deliver")
	}
}

// TestDispatcherDeliverUnknownIDReturnsFalse verifies late replies are
// dropped silently.
func TestDispatcherDeliverUnknownIDReturnsFalse(t *testing.T) {
	d := newDispatcher[*fakeResp]()
	if d.Deliver(9999, &fakeResp{}) {
		t.Fatal("Deliver should return false for unknown id")
	}
}

// TestDispatcherCloseUnblocksWaiters verifies Close closes every pending
// reply channel so callers waiting on them see a zero value and can
// interpret that as "bridge stopped".
func TestDispatcherCloseUnblocksWaiters(t *testing.T) {
	d := newDispatcher[*fakeResp]()
	_, chA := d.Register()
	_, chB := d.Register()

	done := make(chan struct{})
	var got [2]*fakeResp
	go func() {
		got[0] = <-chA
		got[1] = <-chB
		close(done)
	}()

	d.Close()

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Close did not unblock waiters")
	}
	if got[0] != nil || got[1] != nil {
		t.Fatalf("expected zero values after Close, got %+v", got)
	}
}

// TestDispatcherRegisterAfterCloseReturnsClosedChannel verifies a caller
// racing past a concurrent Close does not leak an entry into the pending
// map; instead it receives a closed channel and fires through its select
// immediately.
func TestDispatcherRegisterAfterCloseReturnsClosedChannel(t *testing.T) {
	d := newDispatcher[*fakeResp]()
	d.Close()

	_, ch := d.Register()
	select {
	case resp := <-ch:
		if resp != nil {
			t.Fatalf("expected zero value, got %+v", resp)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("post-Close Register did not return a closed channel")
	}
}

// TestDispatcherCloseIsIdempotent verifies Close can run multiple times
// without panicking on double-close of already-closed channels.
func TestDispatcherCloseIsIdempotent(t *testing.T) {
	d := newDispatcher[*fakeResp]()
	_, _ = d.Register()
	d.Close()
	d.Close() // must not panic
}

// TestDispatcherResetReusesAfterClose verifies Reset reopens a Closed
// dispatcher so supervise() can reuse the same instance across restart
// cycles.
func TestDispatcherResetReusesAfterClose(t *testing.T) {
	d := newDispatcher[*fakeResp]()
	d.Close()
	d.Reset()

	id, ch := d.Register()
	if !d.Deliver(id, &fakeResp{value: 7}) {
		t.Fatal("Deliver after Reset failed")
	}
	select {
	case got := <-ch:
		if got.value != 7 {
			t.Fatalf("got %+v, want {7}", got)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("channel did not deliver after Reset")
	}
}

// TestDispatcherCancelDropsPending verifies Cancel removes the pending
// entry so a subsequent Deliver is a no-op.
func TestDispatcherCancelDropsPending(t *testing.T) {
	d := newDispatcher[*fakeResp]()
	id, _ := d.Register()
	d.Cancel(id)
	if d.Deliver(id, &fakeResp{value: 1}) {
		t.Fatal("Deliver after Cancel should return false")
	}
}

// TestDispatcherConcurrentRegisterDeliver is a smoke test under -race
// that exercises many goroutines Register-then-Deliver at once.
func TestDispatcherConcurrentRegisterDeliver(t *testing.T) {
	d := newDispatcher[*fakeResp]()
	const n = 200
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			id, ch := d.Register()
			// Half deliver from inside the goroutine; half from a helper.
			if i%2 == 0 {
				d.Deliver(id, &fakeResp{value: i})
			} else {
				go d.Deliver(id, &fakeResp{value: i})
			}
			select {
			case got := <-ch:
				if got == nil || got.value != i {
					t.Errorf("i=%d got %+v", i, got)
				}
			case <-time.After(time.Second):
				t.Errorf("i=%d timeout", i)
			}
		}(i)
	}
	wg.Wait()
}
