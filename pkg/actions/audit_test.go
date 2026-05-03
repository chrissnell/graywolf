package actions

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

type fakePruner struct{ calls atomic.Int32 }

func (f *fakePruner) PruneActionInvocations(_ context.Context, _ int, _ time.Duration) (int, error) {
	f.calls.Add(1)
	return 0, nil
}

func TestPrunerLoopFires(t *testing.T) {
	p := &fakePruner{}
	stop := StartAuditPruner(context.Background(), p, AuditPrunerConfig{Interval: 10 * time.Millisecond})
	defer stop()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && p.calls.Load() < 2 {
		time.Sleep(2 * time.Millisecond)
	}
	if p.calls.Load() < 2 {
		t.Fatalf("expected ≥2 calls, got %d", p.calls.Load())
	}
}
