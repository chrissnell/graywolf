package actions

import (
	"context"
	"testing"
)

func TestRegistryRoundTrip(t *testing.T) {
	r := NewExecutorRegistry()
	stub := executorStub{}
	if err := r.Register("stub", stub); err != nil {
		t.Fatal(err)
	}
	got, ok := r.Lookup("stub")
	if !ok || got != stub {
		t.Fatalf("lookup mismatch")
	}
	if err := r.Register("stub", stub); err == nil {
		t.Fatal("expected duplicate-type error")
	}
}

type executorStub struct{}

func (executorStub) Execute(_ context.Context, _ ExecRequest) Result { return Result{Status: StatusOK} }
