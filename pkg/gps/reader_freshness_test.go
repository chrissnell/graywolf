package gps

import (
	"bytes"
	"context"
	"fmt"
	"github.com/chrissnell/graywolf/pkg/metrics"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func freshnessNMEA(body string) string {
	var sum byte
	for i := range body {
		sum ^= body[i]
	}
	return fmt.Sprintf("$%s*%02X\n", body, sum)
}
func TestFreshnessAcrossGPSDAndDirectNMEAReaders(t *testing.T) {
	rmc := func(epoch string) string {
		return freshnessNMEA("GPRMC," + epoch + ",A,4807.038,N,01131.000,E,022.4,084.4,230394,003.1,W")
	}
	gga := func(epoch string) string {
		return freshnessNMEA("GPGGA," + epoch + ",4807.038,N,01131.000,E,1,08,0.9,545.4,M,46.9,M,,")
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cases := []struct {
		name, first, next, nonPosition string
		read                           func(io.Reader, PositionCache) error
	}{
		{"gpsd TPV", `{"class":"TPV","mode":3,"time":"2000-01-01T12:35:19Z","lat":48.1,"lon":11.5}` + "\n", `{"class":"TPV","mode":3,"time":"2000-01-01T12:35:20Z","lat":48.1,"lon":11.5}` + "\n", `{"class":"TPV","mode":1,"time":"2000-01-01T12:35:21Z"}` + "\n", func(r io.Reader, c PositionCache) error {
			return readGPSDStream(context.Background(), r, c, logger, nil, metrics.NewRateLimitedLogger(time.Minute))
		}},
		{"serial or TCP NMEA RMC", rmc("123519"), rmc("123520"), freshnessNMEA("GPVTG,90.0,T,85.0,M,5.5,N,10.2,K,A"), func(r io.Reader, c PositionCache) error {
			return ReadNMEAStream(context.Background(), r, c, logger, NMEAOptions{})
		}},
		{"serial or TCP NMEA GGA", gga("123519"), gga("123520"), freshnessNMEA("GPGSA,A,3,04,05,09,12,24,25,29,31,,,,,1.8,1.0,1.5"), func(r io.Reader, c PositionCache) error {
			return ReadNMEAStream(context.Background(), r, c, logger, NMEAOptions{})
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := NewMemCache()
			read := func(s string) {
				t.Helper()
				if err := tc.read(strings.NewReader(s), c); err != nil {
					t.Fatal(err)
				}
			}
			read(tc.first)
			f, _ := c.Get()
			if !f.ReceivedAt.IsZero() {
				t.Fatal("single report claimed advancing position")
			}
			read(tc.next)
			f, _ = c.Get()
			if f.ReceivedAt.IsZero() {
				t.Fatal("advancing position not fresh")
			}
			at := f.ReceivedAt
			read(tc.next)
			read(tc.nonPosition)
			f, _ = c.Get()
			if !f.ReceivedAt.Equal(at) {
				t.Fatal("duplicate/no-fix/course/DOP report refreshed position age")
			}
		})
	}
}

func TestInterleavedNMEADatesCannotRefreshRepeatedEpochs(t *testing.T) {
	// RMC has a GNSS date; GGA obtains today's date from the unsynchronized host.
	rmc := freshnessNMEA("GPRMC,123519,A,4807.038,N,01131.000,E,022.4,084.4,230394,003.1,W")
	gga := freshnessNMEA("GPGGA,123519,4807.038,N,01131.000,E,1,08,0.9,545.4,M,46.9,M,,")
	c := NewMemCache()
	stream := bytes.NewBufferString(strings.Repeat(rmc+gga, 5))
	if err := ReadNMEAStream(context.Background(), stream, c, slog.New(slog.NewTextHandler(io.Discard, nil)), NMEAOptions{}); err != nil {
		t.Fatal(err)
	}
	f, _ := c.Get()
	if !f.ReceivedAt.IsZero() {
		t.Fatal("alternating cached RMC/GGA timestamps claimed fresh position")
	}
}

func TestFixedSourceUpdatesUseTheSameFreshnessCache(t *testing.T) {
	c := NewMemCache()
	cfg := FixedConfig{Latitude: 39.8, Longitude: -84.8}
	epoch := time.Now()
	c.Update(cfg.fix(epoch))
	c.Update(cfg.fix(epoch.Add(fixedRefreshInterval)))
	f, _ := c.Get()
	if f.ReceivedAt.IsZero() {
		t.Fatal("fixed-source refresh did not advance local receipt")
	}
	if f.Latitude != cfg.Latitude || f.Longitude != cfg.Longitude {
		t.Fatal("fixed coordinates changed")
	}
}
