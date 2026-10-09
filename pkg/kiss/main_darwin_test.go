//go:build darwin && cgo

package kiss

import (
	"os"
	"testing"

	"github.com/chrissnell/graywolf/pkg/mainthread"
)

// IOBluetooth callbacks need the main thread's run loop (see
// pkg/mainthread), so the opt-in hardware test runs the tests off the main
// thread. It is enabled only for that test because the extra goroutine
// would trip the goleak checks in the other tests.
func TestMain(m *testing.M) {
	if os.Getenv("GRAYWOLF_BT_ADDR") == "" {
		os.Exit(m.Run())
	}
	code := 0
	mainthread.Run(func() { code = m.Run() })
	os.Exit(code)
}
