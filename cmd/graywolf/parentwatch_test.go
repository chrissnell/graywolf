package main

import (
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// blockingReader blocks in Read until release is closed, then returns EOF.
// Used so the stdin watcher never fires in tests that target the ppid path.
type blockingReader struct{ release chan struct{} }

func (b *blockingReader) Read(p []byte) (int, error) {
	<-b.release
	return 0, io.EOF
}

func TestWatchParentDeath_StdinEOFFiresOnDeath(t *testing.T) {
	called := make(chan struct{})
	onDeath := func() { close(called) }

	// strings.NewReader("") returns (0, io.EOF) on first Read.
	// Stable ppid + huge poll interval ensure only the stdin path can fire.
	stablePpid := func() int { return 1234 }
	go watchParentDeath(strings.NewReader(""), stablePpid, time.Hour, onDeath)

	select {
	case <-called:
	case <-time.After(2 * time.Second):
		t.Fatal("onDeath was not called on stdin EOF")
	}
}

func TestWatchParentDeath_ReparentPollFiresOnDeath(t *testing.T) {
	called := make(chan struct{})
	onDeath := func() { close(called) }

	// stdin blocks for the whole test so only the ppid path can fire.
	br := &blockingReader{release: make(chan struct{})}
	t.Cleanup(func() { close(br.release) })

	var mu sync.Mutex
	ppid := 1000 // captured as origPpid on the first call
	ppidFn := func() int {
		mu.Lock()
		defer mu.Unlock()
		return ppid
	}

	go watchParentDeath(br, ppidFn, 5*time.Millisecond, onDeath)

	// Let watchParentDeath capture origPpid, then simulate reparenting.
	time.Sleep(20 * time.Millisecond)
	mu.Lock()
	ppid = 1 // reparented to init
	mu.Unlock()

	select {
	case <-called:
	case <-time.After(2 * time.Second):
		t.Fatal("onDeath was not called on ppid change")
	}
}
