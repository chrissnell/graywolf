package gps

import (
	"bytes"
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/metrics"
)

// TestReadGPSDStream_OnParseError verifies that every JSON line that
// fails to unmarshal triggers OnParseError("gpsd"), and that valid
// TPV reports still land in the cache. Uses readGPSDStream directly
// so we don't need a live gpsd socket.
func TestReadGPSDStream_OnParseError(t *testing.T) {
	stream := bytes.NewBufferString(
		// malformed JSON — must count:
		"{\"class\":\"TPV\"\n" +
			// totally non-JSON — must count:
			"garbage\n" +
			// valid but wrong class — parses, does NOT count (not a parse error):
			"{\"class\":\"SKY\"}\n" +
			// valid TPV with fix — must reach the cache, must NOT count:
			"{\"class\":\"TPV\",\"mode\":2,\"lat\":37.5,\"lon\":-122.0}\n",
	)
	cache := NewMemCache()
	logger := slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil))
	parseErrLog := metrics.NewRateLimitedLogger(time.Minute)

	var parseErrs int
	onParseError := func(source string) {
		if source != "gpsd" {
			t.Errorf("source = %q, want %q", source, "gpsd")
		}
		parseErrs++
	}

	if err := readGPSDStream(context.Background(), stream, cache, logger, onParseError, parseErrLog); err != nil {
		t.Fatalf("readGPSDStream: %v", err)
	}
	if parseErrs != 2 {
		t.Errorf("parse errors counted = %d, want 2", parseErrs)
	}
	fix, ok := cache.Get()
	if !ok {
		t.Fatal("valid TPV did not reach cache")
	}
	if !approxEq(fix.Latitude, 37.5, 1e-6) {
		t.Errorf("lat = %v, want 37.5", fix.Latitude)
	}
}
