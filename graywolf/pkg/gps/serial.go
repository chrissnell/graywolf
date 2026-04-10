package gps

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"go.bug.st/serial"
)

// SerialConfig configures a NMEA-over-serial reader.
type SerialConfig struct {
	Device   string // e.g. /dev/ttyUSB0
	BaudRate int    // e.g. 4800, 9600, 38400
}

// RunSerial opens the serial port and feeds NMEA sentences into cache
// until ctx is cancelled. The port is always closed on return. On I/O
// error it returns the error; callers (cmd/graywolf) implement retry
// with backoff.
func RunSerial(ctx context.Context, cfg SerialConfig, cache PositionCache, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.Device == "" {
		return fmt.Errorf("gps: serial device required")
	}
	baud := cfg.BaudRate
	if baud == 0 {
		baud = 4800
	}

	// macOS gotcha: /dev/tty.usbserial-* blocks until DCD is asserted, which
	// most USB GPS devices never assert. Warn loudly so the user can switch
	// to /dev/cu.usbserial-* (the callout device).
	if strings.HasPrefix(cfg.Device, "/dev/tty.usbserial") || strings.HasPrefix(cfg.Device, "/dev/tty.usbmodem") {
		alt := strings.Replace(cfg.Device, "/dev/tty.", "/dev/cu.", 1)
		logger.Warn("gps: macOS /dev/tty.* device blocks on DCD; reads may stall. Try the callout device instead",
			"device", cfg.Device, "suggested", alt)
	}

	mode := &serial.Mode{BaudRate: baud}
	port, err := serial.Open(cfg.Device, mode)
	if err != nil {
		return fmt.Errorf("gps: open %s: %w", cfg.Device, err)
	}
	// Always release the FD on return so retries don't hit EBUSY.
	defer port.Close()

	// Modest read timeout so the scanner goroutine can observe ctx.
	_ = port.SetReadTimeout(500 * time.Millisecond)

	// Close the port when ctx cancels so the blocking read returns immediately.
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = port.Close()
		case <-done:
		}
	}()

	logger.Info("gps serial reader started", "device", cfg.Device, "baud", baud)
	return ReadNMEAStream(ctx, &timeoutReader{ctx: ctx, r: port, logger: logger}, cache, logger)
}

// timeoutReader wraps a serial port whose Read returns (0, nil) on timeout.
// It loops until it gets data, an error, or ctx is cancelled, so bufio.Scanner
// never sees a (0, nil) read (which it would interpret as a stuck stream after
// 100 consecutive empty reads).
type timeoutReader struct {
	ctx     context.Context
	r       io.Reader
	logger  *slog.Logger
	stalled int
}

func (t *timeoutReader) Read(p []byte) (int, error) {
	for {
		if err := t.ctx.Err(); err != nil {
			return 0, io.EOF
		}
		n, err := t.r.Read(p)
		if n > 0 {
			t.stalled = 0
			return n, err
		}
		if err != nil {
			return 0, err
		}
		// (0, nil) — read timeout. Loop, but log periodically so a silent
		// port (wrong baud rate, no device traffic) is visible.
		t.stalled++
		if t.stalled%20 == 0 {
			t.logger.Warn("gps serial: no data received",
				"stalled_reads", t.stalled,
				"hint", "check baud rate, wiring, antenna, or device permissions")
		}
	}
}
