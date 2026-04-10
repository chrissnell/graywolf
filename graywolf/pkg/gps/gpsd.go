package gps

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"time"
)

// GPSDConfig configures a gpsd (TCP JSON) reader.
type GPSDConfig struct {
	Host string // default "localhost"
	Port int    // default 2947
}

// gpsdTPV mirrors the relevant subset of the gpsd TPV (time-position-velocity)
// report. Unused fields are omitted.
type gpsdTPV struct {
	Class string    `json:"class"`
	Mode  int       `json:"mode"` // 0=no mode, 1=no fix, 2=2D, 3=3D
	Time  time.Time `json:"time"`
	Lat   float64   `json:"lat"`
	Lon   float64   `json:"lon"`
	Alt   float64   `json:"alt"`  // metres
	AltM  float64   `json:"altMSL"`
	Speed float64   `json:"speed"` // m/s per gpsd JSON spec
	Track float64   `json:"track"` // degrees true
}

// RunGPSD dials gpsd, issues ?WATCH={"enable":true,"json":true}, and
// feeds TPV reports into cache until ctx is cancelled.
func RunGPSD(ctx context.Context, cfg GPSDConfig, cache PositionCache, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.Default()
	}
	host := cfg.Host
	if host == "" {
		host = "localhost"
	}
	port := cfg.Port
	if port == 0 {
		port = 2947
	}
	addr := fmt.Sprintf("%s:%d", host, port)

	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("gps: gpsd dial %s: %w", addr, err)
	}
	defer conn.Close()

	// Close on cancel so the blocking read returns.
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-done:
		}
	}()

	if _, err := conn.Write([]byte("?WATCH={\"enable\":true,\"json\":true}\n")); err != nil {
		return fmt.Errorf("gps: gpsd watch: %w", err)
	}

	logger.Info("gpsd reader started", "addr", addr)
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 0, 16*1024), 1024*1024)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		line := scanner.Bytes()
		var tpv gpsdTPV
		if err := json.Unmarshal(line, &tpv); err != nil {
			continue
		}
		if tpv.Class != "TPV" {
			continue
		}
		if tpv.Mode < 2 {
			continue
		}
		fix := Fix{
			Latitude:  tpv.Lat,
			Longitude: tpv.Lon,
			Speed:     tpv.Speed * 1.9438444924, // m/s → knots
			Heading:   tpv.Track,
			HasCourse: true,
			Timestamp: tpv.Time,
		}
		if tpv.Mode == 3 {
			alt := tpv.AltM
			if alt == 0 {
				alt = tpv.Alt
			}
			fix.Altitude = alt
			fix.HasAlt = alt != 0
		}
		cache.Update(fix)
	}
	return scanner.Err()
}
