package testtx

import (
	"context"
	"sync"
	"testing"

	"github.com/chrissnell/graywolf/pkg/ax25"
	"github.com/chrissnell/graywolf/pkg/txgovernor"
)

func makeFrame(t *testing.T, info string) *ax25.Frame {
	t.Helper()
	src, err := ax25.ParseAddress("N0CALL-1")
	if err != nil {
		t.Fatal(err)
	}
	dst, err := ax25.ParseAddress("APRS")
	if err != nil {
		t.Fatal(err)
	}
	f, err := ax25.NewUIFrame(src, dst, nil, []byte(info))
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestRecorderCapturesSubmits(t *testing.T) {
	r := NewRecorder()
	if err := r.Submit(context.Background(), 1, makeFrame(t, "a"), txgovernor.SubmitSource{Kind: "kiss"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if err := r.Submit(context.Background(), 2, makeFrame(t, "b"), txgovernor.SubmitSource{Kind: "agw"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if r.Len() != 2 {
		t.Fatalf("Len = %d, want 2", r.Len())
	}
	caps := r.Captures()
	if caps[0].Channel != 1 || caps[1].Channel != 2 {
		t.Errorf("channels out of order: %+v", caps)
	}
	if caps[0].Source.Kind != "kiss" || caps[1].Source.Kind != "agw" {
		t.Errorf("sources wrong: %+v", caps)
	}
	frames := r.Frames()
	if string(frames[0].Info) != "a" || string(frames[1].Info) != "b" {
		t.Errorf("frames: %+v", frames)
	}
	last := r.Last()
	if last == nil || string(last.Frame.Info) != "b" {
		t.Errorf("Last: %+v", last)
	}
}

func TestRecorderHookFiresOutsideLock(t *testing.T) {
	r := NewRecorder()
	var hookCount int
	var mu sync.Mutex
	r.OnSubmit(func(c Capture) {
		// Re-enter an accessor; this would deadlock if the hook were
		// invoked while holding the Recorder lock.
		_ = r.Len()
		mu.Lock()
		hookCount++
		mu.Unlock()
	})
	for i := 0; i < 3; i++ {
		_ = r.Submit(context.Background(), 1, makeFrame(t, "x"), txgovernor.SubmitSource{})
	}
	mu.Lock()
	got := hookCount
	mu.Unlock()
	if got != 3 {
		t.Errorf("hookCount = %d, want 3", got)
	}
}

func TestRecorderConcurrent(t *testing.T) {
	r := NewRecorder()
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 250; i++ {
				_ = r.Submit(context.Background(), 1, makeFrame(t, "x"), txgovernor.SubmitSource{})
			}
		}()
	}
	wg.Wait()
	if r.Len() != 8*250 {
		t.Errorf("len = %d, want %d", r.Len(), 8*250)
	}
}

func TestRecorderReset(t *testing.T) {
	r := NewRecorder()
	_ = r.Submit(context.Background(), 1, makeFrame(t, "x"), txgovernor.SubmitSource{})
	_ = r.Submit(context.Background(), 1, makeFrame(t, "y"), txgovernor.SubmitSource{})
	r.Reset()
	if r.Len() != 0 {
		t.Errorf("after reset: len = %d, want 0", r.Len())
	}
}

// TestRecorderImplementsTxSink checks at compile time and at runtime
// that Recorder satisfies txgovernor.TxSink. If the governor's
// interface ever drifts, this test fails loudly instead of the drift
// being caught only in downstream packages.
func TestRecorderImplementsTxSink(t *testing.T) {
	var _ txgovernor.TxSink = NewRecorder()
}
