//go:build !android && !darwin && !windows && !linux

package main

import "log/slog"

// preventSystemSleep is a no-op on platforms other than macOS, Linux, and Windows.
func preventSystemSleep(_ *slog.Logger) func() {
	return func() {}
}
