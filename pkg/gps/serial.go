package gps

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.bug.st/serial"
)

// SerialConfig configures a NMEA-over-serial reader.
type SerialConfig struct {
	Device   string // e.g. /dev/ttyUSB0
	BaudRate int    // e.g. 4800, 9600, 38400
}

// RunSerial opens the serial port and feeds NMEA sentences into cache
// until ctx is cancelled. On I/O error it closes the port and returns
// the error; callers (cmd/graywolf) are expected to implement retry
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
	mode := &serial.Mode{BaudRate: baud}
	port, err := serial.Open(cfg.Device, mode)
	if err != nil {
		return fmt.Errorf("gps: open %s: %w", cfg.Device, err)
	}
	// Modest read timeout so the scanner goroutine can observe ctx.
	_ = port.SetReadTimeout(500 * time.Millisecond)

	// Close the port when ctx cancels so the blocking read returns.
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
	return ReadNMEAStream(ctx, port, cache, logger)
}
