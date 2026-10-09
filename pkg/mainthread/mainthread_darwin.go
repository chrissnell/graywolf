//go:build darwin && cgo

package mainthread

/*
#cgo LDFLAGS: -framework CoreFoundation
#include <CoreFoundation/CoreFoundation.h>
#include <stdatomic.h>

static atomic_int mt_done;

static void mt_noop(CFRunLoopTimerRef t, void *info) {}

// Runs the calling (main) thread's run loop until mt_stop. A repeating
// timer keeps the loop from finishing when it has no other sources, which
// would make CFRunLoopRunInMode return immediately and spin.
static void mt_run(void) {
    CFRunLoopTimerRef timer = CFRunLoopTimerCreate(
        kCFAllocatorDefault, CFAbsoluteTimeGetCurrent() + 3600, 3600, 0, 0, mt_noop, NULL);
    CFRunLoopAddTimer(CFRunLoopGetCurrent(), timer, kCFRunLoopCommonModes);
    while (!atomic_load(&mt_done)) {
        CFRunLoopRunInMode(kCFRunLoopDefaultMode, 1.0, false);
    }
    CFRunLoopRemoveTimer(CFRunLoopGetCurrent(), timer, kCFRunLoopCommonModes);
    CFRelease(timer);
}

static void mt_stop(void) {
    atomic_store(&mt_done, 1);
    CFRunLoopStop(CFRunLoopGetMain());
}
*/
import "C"

import "runtime"

// Package init runs on the main goroutine, on the process main thread.
// Locking here keeps that goroutine on the main thread until main begins,
// which is what lets Run drive the main run loop from it.
func init() { runtime.LockOSThread() }

// Run calls fn on a new goroutine and services the main thread's run loop
// until fn returns. It must be called from the main goroutine (typically
// as the whole body of main); in a test binary, from TestMain.
func Run(fn func()) {
	done := make(chan struct{})
	go func() {
		defer func() {
			close(done)
			C.mt_stop()
		}()
		fn()
	}()
	running.Store(true)
	C.mt_run()
	running.Store(false)
	<-done
}
