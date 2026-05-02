package actions

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/chrissnell/graywolf/pkg/configstore"
)

// Executor runs one Action invocation. Implementations are stateless
// from the runner's perspective; per-call state lives in ExecRequest /
// Result. Adding a new Action type means writing a new Executor and
// registering it in the registry — no other package needs to change.
type Executor interface {
	// Execute runs the request and returns a Result. The implementation
	// must honor ctx for cancellation/timeout. It must not panic; any
	// internal failure becomes Result{Status: StatusError, ...}.
	Execute(ctx context.Context, req ExecRequest) Result
}

// ExecRequest is the contract between the runner and an Executor. The
// runner builds it from a sanitized invocation + the matched Action.
type ExecRequest struct {
	Action     *configstore.Action
	Invocation Invocation
	Timeout    time.Duration
}

// ExecutorRegistry maps Action.Type strings to concrete Executors.
type ExecutorRegistry struct {
	mu sync.RWMutex
	m  map[string]Executor
}

func NewExecutorRegistry() *ExecutorRegistry {
	return &ExecutorRegistry{m: map[string]Executor{}}
}

func (r *ExecutorRegistry) Register(typeName string, e Executor) error {
	if typeName == "" || e == nil {
		return errors.New("actions: register requires non-empty type and executor")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.m[typeName]; ok {
		return fmt.Errorf("actions: executor type %q already registered", typeName)
	}
	r.m[typeName] = e
	return nil
}

func (r *ExecutorRegistry) Lookup(typeName string) (Executor, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.m[typeName]
	return e, ok
}
