package weather

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"
)

const peetPacketSample = "$ULTW0053002D028D02FA2813000D87BD000103E8015703430010000C"

func TestParsePeetLinePacketMode(t *testing.T) {
	observation, err := ParsePeetLine(peetPacketSample, PeetRain001In)
	if err != nil {
		t.Fatal(err)
	}
	got, err := EncodeAPRS(observation)
	if err != nil {
		t.Fatal(err)
	}
	const want = "064/001g005t065P016h00b10259"
	if got != want {
		t.Fatalf("EncodeAPRS() = %q, want %q", got, want)
	}
}

func TestParsePeetLineDataLogger(t *testing.T) {
	const line = "!!00000066013D000028710166--------0158053201200210"
	observation, err := ParsePeetLine(line, PeetRain001In)
	if err != nil {
		t.Fatal(err)
	}
	got, err := EncodeAPRS(observation)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "144/000") || !strings.Contains(got, "t032") ||
		!strings.Contains(got, "P288") || !strings.Contains(got, "b10353") {
		t.Fatalf("unexpected APRS weather %q", got)
	}
}

func TestParsePeetLineMetricRainCounter(t *testing.T) {
	fields := []string{
		"0000", "0000", "0000", "0000", "0000", "0000", "0000",
		"0000", "01F4", "0001", "0002", "000A", "0000",
	}
	observation, err := ParsePeetLine("$ULTW"+strings.Join(fields, ""), PeetRain01MM)
	if err != nil {
		t.Fatal(err)
	}
	if observation.RainDayMM == nil || *observation.RainDayMM != 1.0 {
		t.Fatalf("RainDayMM = %v, want 1.0", observation.RainDayMM)
	}
}

func TestParsePeetLineRejectsMalformedRecords(t *testing.T) {
	for _, line := range []string{
		"hello",
		"$ULTW0000",
		"!!00000066013D000028710166--------01580532012002ZZ",
	} {
		if _, err := ParsePeetLine(line, PeetRain001In); err == nil {
			t.Fatalf("ParsePeetLine(%q) succeeded", line)
		}
	}
}

type peetTestPort struct {
	*bytes.Reader
	timeout time.Duration
	closed  bool
}

func (p *peetTestPort) SetReadTimeout(timeout time.Duration) error {
	p.timeout = timeout
	return nil
}

func (p *peetTestPort) Close() error {
	p.closed = true
	return nil
}

func TestReadPeetStreamSkipsUnrelatedLines(t *testing.T) {
	port := &peetTestPort{Reader: bytes.NewReader([]byte("partial\r\nnoise\r\n" + peetPacketSample + "\r\n"))}
	observation, err := ReadPeetStream(context.Background(), port, PeetRain001In)
	if err != nil {
		t.Fatal(err)
	}
	if observation.TemperatureC == nil {
		t.Fatal("temperature missing")
	}
	if port.timeout != peetReadTimeout {
		t.Fatalf("timeout = %v, want %v", port.timeout, peetReadTimeout)
	}
	if !port.closed {
		t.Fatal("port was not closed")
	}
}

type peetTimeoutPort struct{}

func (peetTimeoutPort) Read([]byte) (int, error)           { return 0, nil }
func (peetTimeoutPort) Close() error                       { return nil }
func (peetTimeoutPort) SetReadTimeout(time.Duration) error { return nil }

var _ peetSerialPort = peetTimeoutPort{}
var _ io.ReadCloser = peetTimeoutPort{}

func TestReadPeetStreamTimeout(t *testing.T) {
	_, err := ReadPeetStream(context.Background(), peetTimeoutPort{}, PeetRain001In)
	if err == nil || !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("ReadPeetStream() error = %v, want timeout", err)
	}
}
