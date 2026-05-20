//go:build !android

package app

import "github.com/chrissnell/graywolf/pkg/kiss"

// newKissSerialOpenFunc returns nil on desktop builds so SerialSupervisor
// uses the package's defaultSerialOpen (go.bug.st/serial).
func newKissSerialOpenFunc(_ any) kiss.OpenFunc { return nil }

// kissSerialOpenFunc is the build-tag-aware accessor used by wiring.go
// when constructing kiss.SerialConfig. On desktop builds it returns nil
// so SerialSupervisor falls back to defaultSerialOpen.
func (a *App) kissSerialOpenFunc() kiss.OpenFunc { return nil }
