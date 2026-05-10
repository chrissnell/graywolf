//go:build android

package gps

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	pb "github.com/chrissnell/graywolf/pkg/platformproto"
)

// fakeGpsClient implements the subset of platformsvc.Client that
// RunAndroid + RunAndroidGnss depend on. close() must be called from
// t.Cleanup to drain the relay goroutines and prevent leaks under -race.
type fakeGpsClient struct {
	gpsCh  chan *pb.GpsFix
	gnssCh chan *pb.GnssStatusUpdate
}

func (f *fakeGpsClient) SubscribeGpsFix(_ context.Context, ch chan<- *pb.GpsFix) error {
	go func() {
		for fix := range f.gpsCh {
			ch <- fix
		}
	}()
	return nil
}
func (f *fakeGpsClient) SubscribeGnssStatus(_ context.Context, ch chan<- *pb.GnssStatusUpdate) error {
	go func() {
		for st := range f.gnssCh {
			ch <- st
		}
	}()
	return nil
}
func (f *fakeGpsClient) close() {
	if f.gpsCh != nil {
		close(f.gpsCh)
	}
	if f.gnssCh != nil {
		close(f.gnssCh)
	}
}

func TestRunAndroidWritesFixToCache(t *testing.T) {
	cli := &fakeGpsClient{gpsCh: make(chan *pb.GpsFix, 1)}
	t.Cleanup(cli.close)
	cache := NewMemCache()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	go func() { _ = RunAndroid(ctx, cli, cache, logger) }()

	cli.gpsCh <- &pb.GpsFix{
		Lat:        39.7392,
		Lon:        -104.9903,
		AltM:       1609.0,
		HasAlt:     true,
		SpeedMps:   0.5,
		HasSpeed:   true,
		CourseDeg:  142.0,
		HasCourse:  true,
		TimeUnixMs: 1_700_000_000_000,
		AccuracyM:  4.8,
		Source:     pb.GpsSource_GPS_SOURCE_ANDROID_GPS,
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		fix, ok := cache.Get()
		if ok && fix.Latitude == 39.7392 {
			if fix.Longitude != -104.9903 {
				t.Fatalf("unexpected Longitude: %v", fix.Longitude)
			}
			if !fix.HasAlt || fix.Altitude != 1609.0 {
				t.Fatalf("Altitude not propagated: %+v", fix)
			}
			// 0.5 m/s == 0.972... knots
			if fix.Speed < 0.96 || fix.Speed > 0.98 || !fix.HasSpeed {
				t.Fatalf("Speed/HasSpeed not propagated: %+v", fix)
			}
			if fix.Heading != 142.0 || !fix.HasCourse {
				t.Fatalf("Heading not propagated: %+v", fix)
			}
			if fix.AccuracyM != 4.8 {
				t.Fatalf("AccuracyM not propagated: got %v want 4.8", fix.AccuracyM)
			}
			if fix.Timestamp.UnixMilli() != 1_700_000_000_000 {
				t.Fatalf("Timestamp wrong: %v", fix.Timestamp)
			}
			cancel()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timed out waiting for Fix to land in cache")
}

func TestRunAndroidGnssWritesSatelliteView(t *testing.T) {
	cli := &fakeGpsClient{gnssCh: make(chan *pb.GnssStatusUpdate, 1)}
	t.Cleanup(cli.close)
	satCache := NewMemCache()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	go func() { _ = RunAndroidGnss(ctx, cli, satCache, logger) }()

	cli.gnssCh <- &pb.GnssStatusUpdate{
		SatsInView: 11,
		SatsUsed:   8,
		Sats: []*pb.SatInfo{
			{Svid: 5, Constellation: "GPS", Cn0Dbhz: 41.5, UsedInFix: true, ElevationDeg: 32, AzimuthDeg: 145},
			{Svid: 12, Constellation: "GLONASS", Cn0Dbhz: 38.2, UsedInFix: false, ElevationDeg: 11, AzimuthDeg: 220},
		},
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		view, ok := satCache.GetSatellites()
		if ok && len(view.Satellites) == 2 {
			if view.Satellites[0].PRN != 5 || view.Satellites[0].SNR != 41 {
				t.Fatalf("first sat wrong: %+v", view.Satellites[0])
			}
			if !view.Satellites[0].UsedInFix {
				t.Fatalf("UsedInFix not propagated on used sat: %+v", view.Satellites[0])
			}
			if view.Satellites[1].UsedInFix {
				t.Fatalf("UsedInFix=true leaked onto un-used sat: %+v", view.Satellites[1])
			}
			if view.Satellites[0].Constellation != "GPS" {
				t.Fatalf("Constellation not propagated: %q", view.Satellites[0].Constellation)
			}
			if view.SatsInView != 11 || view.SatsUsed != 8 {
				t.Fatalf("totals not propagated: in_view=%d used=%d", view.SatsInView, view.SatsUsed)
			}
			cancel()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timed out waiting for SatelliteView")
}
