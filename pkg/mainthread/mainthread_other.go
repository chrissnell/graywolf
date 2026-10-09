//go:build !(darwin && cgo)

package mainthread

// Run just calls fn: only macOS needs the main thread's run loop.
func Run(fn func()) { fn() }
