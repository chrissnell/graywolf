//go:build android

// Android entry for graywolf. Constructs an app.Config from
// Service-injected env vars (no flags, no signal.Notify -- the
// Android Service owns the process lifecycle), connects to the
// Kotlin PlatformServer for a Hello handshake, then runs
// app.New(cfg).Run(ctx). The HTTP listener gets a per-launch
// bearer-token middleware (invariant N7); a readiness "\n" is
// written to stdout once the listener is bound so the Kotlin
// GoLauncher.startAndAwaitReady gate releases.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/chrissnell/graywolf/pkg/app"
	"github.com/chrissnell/graywolf/pkg/platformsvc"
)

const platformSchemaVersion = 1

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	cfg, err := configFromEnv()
	if err != nil {
		logger.Error("graywolf-android: env parse failed", "err", err)
		os.Exit(2)
	}
	cfg.Version = Version
	cfg.GitCommit = GitCommit

	cfg.OnHTTPListenerReady = func() {
		_, _ = os.Stdout.Write([]byte("\n"))
		_ = os.Stdout.Sync()
		logger.Info("graywolf-android: listener_ready")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := platformHello(ctx, logger, os.Getenv("GRAYWOLF_PLATFORM_SOCKET")); err != nil {
		logger.Error("graywolf-android: platformsvc handshake failed", "err", err)
		os.Exit(1)
	}

	if err := app.New(cfg, logger).Run(ctx); err != nil {
		logger.Error("graywolf-android: exited with error", "err", err)
		os.Exit(1)
	}
}

// platformHello dials the Kotlin PlatformServer at sockPath, exchanges
// Hello, logs the agreed schema version. Mismatch returns an error;
// the Service supervisor will restart the process.
func platformHello(ctx context.Context, logger *slog.Logger, sockPath string) error {
	if sockPath == "" {
		return errors.New("GRAYWOLF_PLATFORM_SOCKET unset")
	}
	cli := platformsvc.NewClient(sockPath)
	defer cli.Close()

	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := cli.ConnectWithReconnect(dialCtx); err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	helloCtx, helloCancel := context.WithTimeout(ctx, 5*time.Second)
	defer helloCancel()
	resp, err := cli.Hello(helloCtx, platformSchemaVersion)
	if err != nil {
		return fmt.Errorf("hello: %w", err)
	}
	logger.Info("platformsvc: connected",
		"server_version", resp.GetServerVersion(),
		"schema_version", resp.GetSchemaVersion())
	if resp.GetSchemaVersion() != platformSchemaVersion {
		return fmt.Errorf("schema mismatch: client=%d server=%d",
			platformSchemaVersion, resp.GetSchemaVersion())
	}
	return nil
}

// Version and GitCommit are linker-injected at build time via -ldflags
// (matching the desktop main.go declarations); the desktop file is
// build-tagged !android so we declare our own copy here.
var (
	Version   = "dev"
	GitCommit = "unknown"
)
