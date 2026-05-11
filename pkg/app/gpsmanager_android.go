//go:build android

package app

import (
	"context"
	"log/slog"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/gps"
)

func platformGpsRunner(a *App, _ *configstore.GPSConfig, logger *slog.Logger, _ func(string)) (gpsRunFunc, string) {
	cli := a.platformClient
	if cli == nil {
		logger.Warn("gps: platformsvc client not available; android reader disabled")
		return nil, ""
	}
	return func(ctx context.Context) error {
		return gps.RunAndroid(ctx, cli, a.gpsCache, logger)
	}, "gps android reader"
}

func platformGnssRunner(a *App, logger *slog.Logger) func(ctx context.Context) error {
	cli := a.platformClient
	if cli == nil {
		return nil
	}
	return func(ctx context.Context) error {
		return gps.RunAndroidGnss(ctx, cli, a.satelliteCache, logger)
	}
}

const platformGpsAlwaysOn = true
