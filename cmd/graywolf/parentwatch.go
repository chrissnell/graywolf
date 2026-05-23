package main

import (
	"io"
	"log/slog"
	"sync"
	"time"
)

// watchParentDeath blocks until it detects that the parent (Android app)
// process has died, then invokes onDeath exactly once and returns. It
// watches two independent signals; whichever fires first wins.
//
//   - stdin EOF: ProcessBuilder hands the Go child a stdin pipe whose
//     write end the JVM holds. When the app process dies (even via
//     SIGKILL) the kernel closes that fd and stdin.Read returns EOF.
//     Event-driven, immediate, OEM-independent.
//   - reparent poll: every pollInterval the current ppid is compared to
//     the one captured at startup. When the app dies the kernel reparents
//     the child to init / a subreaper, changing the ppid. Backstops any
//     case where the stdin pipe behaves unexpectedly.
func watchParentDeath(stdin io.Reader, ppidFn func() int, pollInterval time.Duration, onDeath func()) {
	origPpid := ppidFn()
	done := make(chan struct{})
	var once sync.Once
	fire := func(reason string) {
		once.Do(func() {
			slog.Warn("graywolf-android: parent death detected", "trigger", reason)
			onDeath()
			close(done)
		})
	}

	// stdin EOF watcher.
	go func() {
		buf := make([]byte, 256)
		for {
			if _, err := stdin.Read(buf); err != nil {
				fire("stdin_eof")
				return
			}
		}
	}()

	// reparent poll watcher.
	go func() {
		t := time.NewTicker(pollInterval)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				if ppidFn() != origPpid {
					fire("reparent")
					return
				}
			}
		}
	}()

	<-done
}
