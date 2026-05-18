//go:build android

package gps

import (
	"context"
	"log/slog"
	"time"

	"github.com/chrissnell/graywolf/pkg/platformsvc"
)

// mpsToKnots converts metres per second (Android Location.speed unit)
// to knots (gps.Fix canonical unit, matches APRS / NMEA RMC).
const mpsToKnots = 1.94384449

// AndroidPlatformGpsClient is the subset of platformsvc.Client that the
// android GPS reader needs. Declared as a small interface so unit tests
// can inject a fake without dragging in the whole client surface.
type AndroidPlatformGpsClient interface {
	SubscribeGpsFix(ctx context.Context, ch chan<- *platformsvc.GpsFix) error
	SubscribeGnssStatus(ctx context.Context, ch chan<- *platformsvc.GnssStatusUpdate) error
}

// RunAndroid subscribes to GpsFix events from the platformsvc client
// and pushes each into the provided PositionCache. Returns when ctx is
// cancelled. It does not return on a single subscriber-channel hiccup;
// the platformsvc client manages reconnects upstream.
func RunAndroid(ctx context.Context, cli AndroidPlatformGpsClient, cache PositionCache, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.Default()
	}
	ch := make(chan *platformsvc.GpsFix, 8)
	if err := cli.SubscribeGpsFix(ctx, ch); err != nil {
		return err
	}
	logger.Info("gps android reader started")
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case pbfix := <-ch:
			if pbfix == nil {
				continue
			}
			cache.Update(toFix(pbfix))
		}
	}
}

// RunAndroidGnss subscribes to per-sat GnssStatusUpdate events and
// pushes each into the SatelliteCache. Pairs with RunAndroid.
func RunAndroidGnss(ctx context.Context, cli AndroidPlatformGpsClient, satCache SatelliteCache, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.Default()
	}
	ch := make(chan *platformsvc.GnssStatusUpdate, 4)
	if err := cli.SubscribeGnssStatus(ctx, ch); err != nil {
		return err
	}
	logger.Info("gps android gnss-status reader started")
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case st := <-ch:
			if st == nil {
				continue
			}
			satCache.UpdateSatellites(toSatelliteView(st))
		}
	}
}

func toFix(pb *platformsvc.GpsFix) Fix {
	// Read the proto presence bits directly. Earlier drafts derived
	// HasAlt from "AltM != 0" but legitimate zero values exist
	// (sea-level altitude, due-north course, stationary speed) and
	// derived presence silently dropped them; the proto carries
	// has_alt / has_speed / has_course explicitly.
	return Fix{
		Latitude:  pb.GetLat(),
		Longitude: pb.GetLon(),
		Altitude:  pb.GetAltM(),
		HasAlt:    pb.GetHasAlt(),
		// Android delivers speed in m/s; gps.Fix carries knots.
		Speed:     pb.GetSpeedMps() * mpsToKnots,
		HasSpeed:  pb.GetHasSpeed(),
		Heading:   pb.GetCourseDeg(),
		HasCourse: pb.GetHasCourse(),
		AccuracyM: pb.GetAccuracyM(),
		Timestamp: time.UnixMilli(pb.GetTimeUnixMs()).UTC(),
		FixMode:   3, // Android only delivers Locations after a real fix.
	}
}

func toSatelliteView(st *platformsvc.GnssStatusUpdate) SatelliteView {
	out := SatelliteView{
		UpdatedAt:  time.Now().UTC(),
		SatsInView: int(st.GetSatsInView()),
		SatsUsed:   int(st.GetSatsUsed()),
		Satellites: make([]SatelliteInfo, 0, len(st.GetSats())),
	}
	for _, s := range st.GetSats() {
		// Cn0Dbhz is float64 (e.g. 41.5 dB-Hz) but SatelliteInfo.SNR is
		// int — the truncated dB-Hz is plenty of resolution for the bar
		// display. If a future polar plot needs half-dB precision,
		// widen SatelliteInfo.SNR to float32 in a follow-up.
		out.Satellites = append(out.Satellites, SatelliteInfo{
			PRN:           int(s.GetSvid()),
			Elevation:     int(s.GetElevationDeg()),
			Azimuth:       int(s.GetAzimuthDeg()),
			SNR:           int(s.GetCn0Dbhz()),
			UsedInFix:     s.GetUsedInFix(),
			Constellation: s.GetConstellation(),
		})
	}
	return out
}
