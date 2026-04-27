// Command smoke-kiss-tcp-client is the in-process driver half of the
// scripts/smoke/remote-kiss-tnc.sh end-to-end smoke test.
//
// It assumes a KISS TCP server is already listening at the address
// passed via -peer. It then:
//
//  1. Creates a kiss.Manager and starts a tcp-client supervisor against
//     that peer, with Mode=TNC and AllowTxFromGovernor=true (same
//     configuration as a newly-created tcp-client row via the UI).
//  2. Waits for the supervisor to transition to StateConnected.
//  3. Enqueues a single AX.25 UI frame on the configured channel via
//     Manager.TransmitOnChannel — the same API the TX backend
//     dispatcher uses at runtime.
//  4. Exits 0 on success, non-zero with a descriptive message on any
//     step failure.
//
// The program is deliberately minimal — it wires just the kiss.Manager
// and the per-instance tx queue, bypassing the modem subprocess, the
// configstore, and the HTTP layer. The smoke test validates the kiss
// tcp-client supervisor and per-instance tx queue in isolation, which
// together form the end of the TX dispatch path introduced in Phase 4.
//
// This binary is not shipped in release packages; it exists only to
// back the smoke script.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/chrissnell/graywolf/pkg/ax25"
	"github.com/chrissnell/graywolf/pkg/kiss"
)

func main() {
	peer := flag.String("peer", "", "host:port of the mock KISS TCP server (required)")
	channel := flag.Uint("channel", 11, "channel number for the tcp-client interface")
	srcCall := flag.String("src", "N0CALL", "AX.25 source callsign")
	dstCall := flag.String("dst", "APRS", "AX.25 destination callsign")
	info := flag.String("info", "smoke test: remote kiss tnc", "AX.25 info field payload")
	connectTimeout := flag.Duration("connect-timeout", 5*time.Second, "max wait for supervisor to reach connected state")
	postWriteWait := flag.Duration("post-write-wait", 500*time.Millisecond, "wait after enqueue so the peer has time to read before we tear down")
	verbose := flag.Bool("v", false, "verbose stderr logging")
	flag.Parse()

	if *peer == "" {
		fmt.Fprintln(os.Stderr, "smoke: -peer is required (host:port)")
		os.Exit(2)
	}

	var logger *slog.Logger
	if *verbose {
		logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	} else {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	host, port, err := parseHostPort(*peer)
	if err != nil {
		fmt.Fprintf(os.Stderr, "smoke: parse -peer: %v\n", err)
		os.Exit(2)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mgr := kiss.NewManager(kiss.ManagerConfig{
		Logger: logger,
	})
	defer mgr.StopAll()

	const ifaceID = uint32(1)

	mgr.StartClient(ctx, ifaceID, kiss.ClientConfig{
		InterfaceID:         ifaceID,
		Name:                "smoke-tcp-client",
		RemoteHost:          host,
		RemotePort:          port,
		ReconnectInitMs:     250,
		ReconnectMaxMs:      2000,
		Mode:                kiss.ModeTnc,
		AllowTxFromGovernor: true,
		ChannelMap:          map[uint8]uint32{0: uint32(*channel)},
		// RxIngress is intentionally nil: the smoke test only
		// exercises TX. A nil RxIngress causes inbound frames to
		// drop with a warn log, which is fine for this path.
	})

	// Wait for the supervisor to reach StateConnected.
	deadline := time.Now().Add(*connectTimeout)
	for time.Now().Before(deadline) {
		st := mgr.Status()
		if s, ok := st[ifaceID]; ok && s.State == kiss.StateConnected {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	st := mgr.Status()[ifaceID]
	if st.State != kiss.StateConnected {
		fmt.Fprintf(os.Stderr, "smoke: supervisor did not connect within %s (state=%q last_error=%q)\n",
			*connectTimeout, st.State, st.LastError)
		os.Exit(1)
	}

	// Build a valid AX.25 UI frame and enqueue for transmit.
	srcA, err := ax25.ParseAddress(*srcCall)
	if err != nil {
		fmt.Fprintf(os.Stderr, "smoke: parse -src: %v\n", err)
		os.Exit(2)
	}
	dstA, err := ax25.ParseAddress(*dstCall)
	if err != nil {
		fmt.Fprintf(os.Stderr, "smoke: parse -dst: %v\n", err)
		os.Exit(2)
	}
	frame, err := ax25.NewUIFrame(srcA, dstA, nil, []byte(*info))
	if err != nil {
		fmt.Fprintf(os.Stderr, "smoke: build ui frame: %v\n", err)
		os.Exit(1)
	}
	axBytes, err := frame.Encode()
	if err != nil {
		fmt.Fprintf(os.Stderr, "smoke: encode ax.25: %v\n", err)
		os.Exit(1)
	}

	accepted, err := mgr.TransmitOnChannel(ctx, uint32(*channel), axBytes, 1)
	if err != nil {
		fmt.Fprintf(os.Stderr, "smoke: TransmitOnChannel err=%v accepted=%d\n", err, accepted)
		os.Exit(1)
	}
	if accepted != 1 {
		fmt.Fprintf(os.Stderr, "smoke: TransmitOnChannel accepted=%d, want 1\n", accepted)
		os.Exit(1)
	}

	// Give the drainer goroutine time to actually write the KISS
	// frame to the socket before we tear down. 500ms is generous.
	time.Sleep(*postWriteWait)

	fmt.Fprintln(os.Stderr, "smoke: one frame submitted and accepted by the tcp-client instance queue")
}

func parseHostPort(s string) (string, uint16, error) {
	// net.SplitHostPort is overkill for the smoke harness; a simple
	// strings.LastIndex of ':' handles the documented -peer=host:port
	// form. IPv6 with brackets is not expected from the smoke script.
	i := -1
	for k := len(s) - 1; k >= 0; k-- {
		if s[k] == ':' {
			i = k
			break
		}
	}
	if i <= 0 || i == len(s)-1 {
		return "", 0, fmt.Errorf("expected host:port, got %q", s)
	}
	var p uint16
	for _, c := range s[i+1:] {
		if c < '0' || c > '9' {
			return "", 0, fmt.Errorf("non-numeric port in %q", s)
		}
		p = p*10 + uint16(c-'0')
	}
	return s[:i], p, nil
}
