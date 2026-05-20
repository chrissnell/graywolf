//go:build android

package app

import (
	"context"
	"errors"
	"io"
	"regexp"

	"github.com/chrissnell/graywolf/pkg/kiss"
)

// btSerialOpener is the narrow surface the factory needs from platformsvc.
// Defining it here makes the factory unit-testable without dragging the
// full platformsvc.Client surface into the test.
type btSerialOpener interface {
	BtSerialOpen(ctx context.Context, mac string) (io.ReadWriteCloser, error)
}

// macRe matches MAC-48 in either colon-separated or hyphen-separated form.
var macRe = regexp.MustCompile(`^([0-9A-Fa-f]{2}[:-]){5}[0-9A-Fa-f]{2}$`)

// errNotSupportedOnAndroid is returned when wiring tries to open a raw
// serial device path on Android. The Android sandbox blocks /dev/tty*
// anyway, so this surfaces the configuration mistake immediately rather
// than letting it fail later inside go.bug.st/serial.
var errNotSupportedOnAndroid = errors.New("kiss: serial device path not supported on Android (use Bluetooth interface)")

// newKissSerialOpenFunc returns an OpenFunc that routes MAC-style device
// strings to platformsvc.BtSerialOpen and rejects raw device paths.
// Returns nil when psv is nil so the supervisor falls back to its default
// open (which on Android would also fail, but lets the supervisor surface
// the error consistently).
func newKissSerialOpenFunc(psv btSerialOpener) kiss.OpenFunc {
	if psv == nil {
		return nil
	}
	return func(device string, _ uint32) (io.ReadWriteCloser, error) {
		if macRe.MatchString(device) {
			return psv.BtSerialOpen(context.Background(), device)
		}
		return nil, errNotSupportedOnAndroid
	}
}

// kissSerialOpenFunc is the build-tag-aware accessor used by wiring.go
// when constructing kiss.SerialConfig. On Android it returns an OpenFunc
// that routes MAC-style device strings through the injected platformsvc
// client; if no platform client is wired yet, it returns nil.
func (a *App) kissSerialOpenFunc() kiss.OpenFunc {
	if a.platformClient == nil {
		return nil
	}
	return newKissSerialOpenFunc(a.platformClient)
}
