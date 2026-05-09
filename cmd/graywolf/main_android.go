//go:build android

// Phase 2 stub. Phase 3 replaces this with the real Android entry that
// wires pkg/platformsvc, pkg/gps/android, and pkg/pttdevice/android into
// the main app graph. Until then, this stub exists solely so cargo-ndk's
// sibling `go build ./...` cross-compile of the multi-ABI APK pipeline
// terminates without unresolved symbols.
package main

func main() {}
