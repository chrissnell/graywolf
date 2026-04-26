// graywolf entry point. All runtime wiring lives in pkg/app; this file
// is a thin dispatch shim responsible for build-time version injection,
// subcommand routing, and signal handling. The normal-path main() body
// is app.New(cfg, logger).Run(ctx).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/chrissnell/graywolf/cmd/graywolf/authcli"
	"github.com/chrissnell/graywolf/pkg/app"
	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/logbuffer"
)

// Version and GitCommit are injected at build time via -ldflags. Both
// sides of the build (Go + Rust) format their display string as
// "v<Version>-<GitCommit>"; the Rust side must produce a byte-identical
// string so the startup banner's mismatch check works.
var (
	Version   = "dev"
	GitCommit = "unknown"
)

func fullVersion() string {
	return fmt.Sprintf("v%s-%s", Version, GitCommit)
}

func main() {
	// Pre-flag-parse logger: subcommands (auth, version) only need a
	// minimal handler. The full logbuffer-wrapped handler is constructed
	// after ParseFlags so we know cfg.DBPath and cfg.LogBufferRamdisk.
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "auth":
			if err := authcli.Run(os.Args[2:], logger, Version); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
			return
		case "version":
			fmt.Println(fullVersion())
			return
		}
	}

	cfg, err := app.ParseFlags(os.Args[1:])
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(2)
	}
	cfg.Version = Version
	cfg.GitCommit = GitCommit

	innerLevel := slog.LevelInfo
	if cfg.Debug {
		innerLevel = slog.LevelDebug
	}
	inner := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: innerLevel})

	logger = setupLogger(inner, cfg)
	slog.SetDefault(logger)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := app.New(cfg, logger).Run(ctx); err != nil {
		logger.Error("graywolf exited with error", "err", err)
		os.Exit(1)
	}
}

// setupLogger wraps inner with a logbuffer.Handler that persists records
// to a standalone SQLite ring. Path selection is environment-aware
// (Pi/SD-card -> ramdisk; everywhere else -> next to graywolf.db). If
// the ring DB cannot be opened we log a warning through the inner
// handler and return it unwrapped -- graywolf must boot even when the
// ring is unavailable.
//
// The configstore override (logbuffer_configs.max_rows) is read here
// best-effort: a missing or unreadable configstore yields the
// environment default. The override takes effect on the next graywolf
// start; live reload is intentionally out of scope.
func setupLogger(inner slog.Handler, cfg app.Config) *slog.Logger {
	const (
		ringSizeRamdisk     = 2000
		ringSizeDisk        = 5000
		maintenanceInterval = 200
	)

	var (
		isPi     = logbuffer.IsRaspberryPiHost()
		dbDev, _ = logbuffer.BackingDeviceFor(cfg.DBPath)
		isSD     = logbuffer.IsSDCardDevice(dbDev)
	)
	target, err := logbuffer.ResolvePath(logbuffer.ResolveOptions{
		ConfigDBPath:    cfg.DBPath,
		PreferRamdisk:   cfg.LogBufferRamdisk,
		IsRaspberryPi:   isPi,
		BackingIsSDCard: isSD,
	})
	if err != nil {
		warn(inner, "logbuffer: path resolution failed; falling back to console-only", "err", err)
		return slog.New(inner)
	}

	db, err := logbuffer.Open(target)
	if err != nil {
		warn(inner, "logbuffer: open failed; falling back to console-only", "err", err, "path", target)
		return slog.New(inner)
	}

	wantedRamdisk := isPi || isSD || cfg.LogBufferRamdisk
	if wantedRamdisk && !pathIsRamdisk(target) {
		// Spec § Subsystem 1: "Fall back to disk with a WARN log if no
		// ramdisk is writable." Surface this through the inner handler
		// since SetDefault hasn't run yet.
		warn(inner, "logbuffer: no ramdisk writable; falling back to disk-backed ring", "path", target)
	}

	ringSize := ringSizeDisk
	if wantedRamdisk {
		ringSize = ringSizeRamdisk
	}
	override, hasOverride := readMaxRowsOverride(cfg.DBPath)
	if hasOverride {
		if override <= 0 {
			// Operator explicitly disabled persistence.
			_ = db.Close()
			warn(inner, "logbuffer: persistence disabled by configstore (logbuffer.max_rows=0)")
			return slog.New(inner)
		}
		ringSize = override
	}

	h := logbuffer.New(inner, db, logbuffer.Config{
		RingSize:         ringSize,
		MaintenanceEvery: maintenanceInterval,
	})
	logger := slog.New(h)
	logger.Info("logbuffer: persistence enabled", "path", target, "ring_size", ringSize)
	return logger
}

// pathIsRamdisk reports whether p lives under one of the tmpfs
// candidates the resolver tries. Used by setupLogger to detect "wanted
// ramdisk, didn't get one" so the spec's fallback WARN can fire.
func pathIsRamdisk(p string) bool {
	for _, dir := range []string{"/run/graywolf/", "/dev/shm/graywolf/"} {
		if len(p) >= len(dir) && p[:len(dir)] == dir {
			return true
		}
	}
	return false
}

// warn emits a single warning through the inner handler. Used during
// setup before slog.SetDefault is called.
func warn(inner slog.Handler, msg string, attrs ...any) {
	tmp := slog.New(inner)
	tmp.Warn(msg, attrs...)
}

// readMaxRowsOverride opens the configstore (read-only intent) and
// returns the operator's max_rows override along with a "has-override"
// flag. The flag is critical because MaxRows == 0 with hasOverride
// means "operator opted out of persistence" -- distinct from "no
// override stored, use environment default". Errors are intentionally
// swallowed: a misconfigured configstore should not prevent graywolf
// from booting; the rest of the program will fail with a clearer error
// when it tries to use the configstore for real.
func readMaxRowsOverride(dbPath string) (int, bool) {
	store, err := configstore.Open(dbPath)
	if err != nil {
		return 0, false
	}
	defer store.Close()
	cfg, exists, err := store.GetLogBufferConfig(context.Background())
	if err != nil || !exists {
		return 0, false
	}
	return cfg.MaxRows, true
}
