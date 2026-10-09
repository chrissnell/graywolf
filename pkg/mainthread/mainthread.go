// Package mainthread runs a program's work off the process main thread so
// the main thread can service its CoreFoundation run loop.
//
// macOS frameworks deliver some callbacks only on the main thread's run
// loop regardless of which thread started the operation. IOBluetooth is one:
// RFCOMM open/data/close delegate callbacks never arrive in a Go program
// whose main thread is not running a run loop. Run fixes that.
package mainthread

import "sync/atomic"

var running atomic.Bool

// Running reports whether Run is currently servicing the main run loop.
func Running() bool { return running.Load() }
