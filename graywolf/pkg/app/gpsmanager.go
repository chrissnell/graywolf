package app

import (
	"context"
	"log/slog"
	"time"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/gps"
)

// gpsReaderShutdownGrace bounds how long the manager will wait for a
// previous reader to exit before starting a replacement.
const gpsReaderShutdownGrace = 2 * time.Second

// gpsReaderRestartBackoff is the delay between per-error restarts of a
// reader inside the same configuration epoch.
const gpsReaderRestartBackoff = 5 * time.Second

// gpsRunFunc runs a specific GPS source (serial or gpsd) until ctx is
// cancelled or a real error occurs.
type gpsRunFunc func(ctx context.Context) error

// gpsManager supervises a single GPS reader goroutine, restarting it on
// config reload or per-error backoff. Only the Run goroutine mutates
// its fields, so no mutex is needed.
type gpsManager struct {
	store  *configstore.Store
	cache  *gps.MemCache
	logger *slog.Logger

	cancel context.CancelFunc
	done   chan struct{}
}

func newGPSManager(store *configstore.Store, cache *gps.MemCache, logger *slog.Logger) *gpsManager {
	return &gpsManager{store: store, cache: cache, logger: logger}
}

// Run drives the manager until ctx is cancelled: start on boot, restart
// on reload. Blocks.
func (m *gpsManager) Run(ctx context.Context, reload <-chan struct{}) {
	m.start(ctx)
	for {
		select {
		case <-ctx.Done():
			m.stop()
			return
		case <-reload:
			m.logger.Info("gps config changed, restarting reader")
			m.start(ctx)
		}
	}
}

// start tears down any previous reader, re-reads config, and spawns a
// new reader goroutine if the source is valid and enabled.
func (m *gpsManager) start(parent context.Context) {
	m.stop()

	gpsCfg, err := m.store.GetGPSConfig(parent)
	if err != nil || gpsCfg == nil || !gpsCfg.Enabled {
		m.logger.Info("gps reader disabled")
		return
	}

	var run gpsRunFunc
	var name string
	switch gpsCfg.SourceType {
	case "serial":
		scfg := gps.SerialConfig{Device: gpsCfg.Device, BaudRate: int(gpsCfg.BaudRate)}
		run = func(ctx context.Context) error { return gps.RunSerial(ctx, scfg, m.cache, m.logger) }
		name = "gps serial reader"
	case "gpsd":
		gcfg := gps.GPSDConfig{Host: gpsCfg.GpsdHost, Port: int(gpsCfg.GpsdPort)}
		run = func(ctx context.Context) error { return gps.RunGPSD(ctx, gcfg, m.cache, m.logger) }
		name = "gpsd reader"
	default:
		m.logger.Info("gps source type not recognized", "type", gpsCfg.SourceType)
		return
	}

	readerCtx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	m.cancel = cancel
	m.done = done
	go m.runLoop(readerCtx, done, run, name)
}

// runLoop is the per-reader supervisor: call run, log any non-cancel
// error, and back off before retrying. Exits when ctx is cancelled.
func (m *gpsManager) runLoop(ctx context.Context, done chan struct{}, run gpsRunFunc, name string) {
	defer close(done)
	for {
		err := run(ctx)
		if ctx.Err() != nil {
			return
		}
		m.logger.Warn(name+" exited", "err", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(gpsReaderRestartBackoff):
		}
	}
}

// stop cancels the current reader and waits for its goroutine to exit,
// bounded by gpsReaderShutdownGrace so a stuck reader cannot block the
// manager indefinitely. Safe to call when no reader is running.
func (m *gpsManager) stop() {
	if m.cancel == nil {
		return
	}
	m.cancel()
	done := m.done
	m.cancel = nil
	m.done = nil
	if done == nil {
		return
	}
	select {
	case <-done:
	case <-time.After(gpsReaderShutdownGrace):
		m.logger.Warn("previous gps reader did not exit in time; starting new reader anyway")
	}
}
