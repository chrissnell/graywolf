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
	"net"
	"os"
	"strings"
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

	// Override Go's default DNS resolver with the server list the
	// Service captured from ConnectivityManager. Without this, every
	// outbound DNS lookup (maps catalog, auth.nw5w.com registration,
	// rotate.aprs2.net iGate, beacon paths) dials [::1]:53 and fails
	// with "connection refused" because Android doesn't have
	// /etc/resolv.conf and Go's pure-Go resolver has no other source.
	// This single override covers everything that goes through net.Dial.
	if servers := os.Getenv("GRAYWOLF_DNS_SERVERS"); servers != "" {
		installAndroidResolver(logger, servers)
	} else {
		logger.Warn("graywolf-android: no GRAYWOLF_DNS_SERVERS; outbound DNS will fail")
	}

	cfg.OnHTTPListenerReady = func() {
		_, _ = os.Stdout.Write([]byte("\n"))
		_ = os.Stdout.Sync()
		logger.Info("graywolf-android: listener_ready")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a := app.New(cfg, logger)

	cli, err := platformConnect(ctx, logger, os.Getenv("GRAYWOLF_PLATFORM_SOCKET"))
	if err != nil {
		logger.Error("graywolf-android: platformsvc handshake failed", "err", err)
		os.Exit(1)
	}
	defer cli.Close()
	a.SetPlatformClient(cli)

	if err := a.Run(ctx); err != nil {
		logger.Error("graywolf-android: exited with error", "err", err)
		os.Exit(1)
	}
}

// platformConnect dials the Kotlin PlatformServer at sockPath, exchanges
// Hello, logs the agreed schema version, and returns the live client
// for the app to keep using. The caller owns Close. Mismatch returns
// an error; the Service supervisor will restart the process.
func platformConnect(ctx context.Context, logger *slog.Logger, sockPath string) (platformsvc.Client, error) {
	if sockPath == "" {
		return nil, errors.New("GRAYWOLF_PLATFORM_SOCKET unset")
	}
	cli := platformsvc.NewClient(sockPath)

	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := cli.ConnectWithReconnect(dialCtx); err != nil {
		_ = cli.Close()
		return nil, fmt.Errorf("connect: %w", err)
	}
	helloCtx, helloCancel := context.WithTimeout(ctx, 5*time.Second)
	defer helloCancel()
	resp, err := cli.Hello(helloCtx, platformSchemaVersion)
	if err != nil {
		_ = cli.Close()
		return nil, fmt.Errorf("hello: %w", err)
	}
	logger.Info("platformsvc: connected",
		"server_version", resp.GetServerVersion(),
		"schema_version", resp.GetSchemaVersion())
	if resp.GetSchemaVersion() != platformSchemaVersion {
		_ = cli.Close()
		return nil, fmt.Errorf("schema mismatch: client=%d server=%d",
			platformSchemaVersion, resp.GetSchemaVersion())
	}
	return cli, nil
}

// installAndroidResolver replaces net.DefaultResolver with one whose
// dialer hard-codes the address list from `serverList` (a comma-separated
// list of host[:port] strings as produced by GraywolfService's
// currentDnsServers helper). The default port is :53 when one isn't
// supplied. Servers are tried in order on each lookup; first success wins.
func installAndroidResolver(logger *slog.Logger, serverList string) {
	servers := parseDNSServers(serverList)
	if len(servers) == 0 {
		logger.Warn("graywolf-android: GRAYWOLF_DNS_SERVERS empty after parse")
		return
	}
	logger.Info("graywolf-android: installing DNS resolver", "servers", servers)

	d := &net.Dialer{Timeout: 5 * time.Second}
	net.DefaultResolver = &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var lastErr error
			for _, srv := range servers {
				conn, err := d.DialContext(ctx, network, srv)
				if err == nil {
					return conn, nil
				}
				lastErr = err
			}
			if lastErr != nil {
				return nil, lastErr
			}
			return nil, errors.New("no DNS servers configured")
		},
	}
}

// parseDNSServers splits a comma-separated server list (as produced by
// GraywolfService.currentDnsServers) into "host:port" entries. Servers
// missing a :port suffix get :53 appended. IPv6 literals must arrive
// already bracketed (e.g. "[2001:db8::1]" or "[2001:db8::1]:5353").
func parseDNSServers(raw string) []string {
	out := []string{}
	for _, p := range strings.Split(raw, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		// Already host:port? net.SplitHostPort succeeds on bracketed v6
		// with a port too.
		if _, _, err := net.SplitHostPort(p); err == nil {
			out = append(out, p)
			continue
		}
		// Bracketed v6 without port → append :53.
		if strings.HasPrefix(p, "[") && strings.HasSuffix(p, "]") {
			out = append(out, p+":53")
			continue
		}
		out = append(out, p+":53")
	}
	return out
}

// Version and GitCommit are linker-injected at build time via -ldflags
// (matching the desktop main.go declarations); the desktop file is
// build-tagged !android so we declare our own copy here.
var (
	Version   = "dev"
	GitCommit = "unknown"
)
