//go:build !android

package app

import (
	"context"
	"log/slog"

	"github.com/chrissnell/graywolf/pkg/configstore"
)

// platformGpsRunner returns a non-nil run func when the host platform
// owns a built-in GPS source (Android). Desktop builds always return
// nil so the gpsManager falls through to its configstore-driven switch.
func platformGpsRunner(_ *App, _ *configstore.GPSConfig, _ *slog.Logger, _ func(string)) (gpsRunFunc, string) {
	return nil, ""
}

// platformGnssRunner mirrors platformGpsRunner for the per-sat companion.
func platformGnssRunner(_ *App, _ *slog.Logger) func(ctx context.Context) error {
	return nil
}

// platformGpsAlwaysOn reports whether the host platform supplies an
// always-on GPS source that should run regardless of configstore-side
// "Enabled" toggle. Android: true; desktop: false (operator opts in).
const platformGpsAlwaysOn = false
