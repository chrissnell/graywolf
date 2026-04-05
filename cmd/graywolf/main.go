// graywolf is the Phase 1 Go entry point: it opens the configstore, starts
// the modem bridge, and exposes /metrics on 127.0.0.1:8080.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/metrics"
	"github.com/chrissnell/graywolf/pkg/modembridge"
)

func main() {
	var (
		dbPath     = flag.String("config", "./graywolf.db", "path to SQLite config database")
		modemPath  = flag.String("modem", "./target/release/graywolf-modem", "path to graywolf-modem binary")
		httpAddr   = flag.String("http", "127.0.0.1:8080", "HTTP listen address for /metrics")
		shutdownTO = flag.Duration("shutdown-timeout", 10*time.Second, "max time to wait for clean shutdown")
	)
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	store, err := configstore.Open(*dbPath)
	if err != nil {
		logger.Error("open configstore", "err", err, "path", *dbPath)
		os.Exit(1)
	}
	defer store.Close()

	m := metrics.New()

	bridge := modembridge.New(modembridge.Config{
		BinaryPath: *modemPath,
		Store:      store,
		Metrics:    m,
		Logger:     logger,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := bridge.Start(ctx); err != nil {
		logger.Error("start modembridge", "err", err)
		os.Exit(1)
	}

	// Drain frames — later phases will route them to KISS/AGW/APRS. For
	// phase 1 we log the count via metrics and discard the payload.
	go func() {
		for range bridge.Frames() {
		}
	}()

	mux := http.NewServeMux()
	mux.Handle("/metrics", m.Handler())
	httpSrv := &http.Server{Addr: *httpAddr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		logger.Info("http listening", "addr", *httpAddr)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http server", "err", err)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh
	logger.Info("shutdown signal received", "signal", sig.String())

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), *shutdownTO)
	defer shutdownCancel()

	cancel() // tell bridge to begin shutdown
	done := make(chan struct{})
	go func() { bridge.Stop(); close(done) }()
	select {
	case <-done:
	case <-shutdownCtx.Done():
		logger.Warn("modembridge shutdown timed out")
	}

	_ = httpSrv.Shutdown(shutdownCtx)
}
