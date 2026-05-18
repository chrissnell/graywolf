package app

import (
	"testing"
)

// TestPlatformGating_AndroidSkipsUpdatescheck is a sentinel for the
// platform-gating decision made in wireHTTP. Real coverage of the
// runtime path comes from the cross-compile probe (GOOS=android
// builds clean) plus manual logcat inspection in the run report.
func TestPlatformGating_AndroidSkipsUpdatescheck(t *testing.T) {
	t.Skip("integration test — wireServices needs a real configstore; revisit if checker.go gains a unit-testable surface")
}
