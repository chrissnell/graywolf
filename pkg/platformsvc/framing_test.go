//go:build android

package platformsvc

import (
	"bytes"
	"errors"
	"io"
	"testing"

	pb "github.com/chrissnell/graywolf/pkg/platformproto"
)

func TestFramingRoundTrip(t *testing.T) {
	msg := &pb.PlatformMessage{
		Body: &pb.PlatformMessage_Hello{
			Hello: &pb.Hello{SchemaVersion: 1, ClientVersion: "v0.0.0-test"},
		},
	}
	var buf bytes.Buffer
	if err := writeFrame(&buf, msg); err != nil {
		t.Fatalf("writeFrame: %v", err)
	}
	got, err := readFrame(&buf)
	if err != nil {
		t.Fatalf("readFrame: %v", err)
	}
	if got.GetHello().GetSchemaVersion() != 1 {
		t.Errorf("schema_version: got %d, want 1", got.GetHello().GetSchemaVersion())
	}
	if got.GetHello().GetClientVersion() != "v0.0.0-test" {
		t.Errorf("client_version mismatch")
	}
}

func TestFramingEmptyPayload(t *testing.T) {
	msg := &pb.PlatformMessage{}
	var buf bytes.Buffer
	if err := writeFrame(&buf, msg); err != nil {
		t.Fatalf("writeFrame: %v", err)
	}
	if got, err := readFrame(&buf); err != nil || got == nil {
		t.Fatalf("readFrame empty: err=%v got=%v", err, got)
	}
}

func TestFramingTruncatedHeader(t *testing.T) {
	r := bytes.NewReader([]byte{0x00, 0x01})
	if _, err := readFrame(r); err == nil {
		t.Fatal("expected truncated-header error, got nil")
	}
}

func TestFramingTruncatedPayload(t *testing.T) {
	r := bytes.NewReader([]byte{0x00, 0x00, 0x00, 0x10, 0x01, 0x02})
	if _, err := readFrame(r); err == nil {
		t.Fatal("expected truncated-payload error, got nil")
	}
}

func TestFramingOversizedRejected(t *testing.T) {
	r := bytes.NewReader([]byte{0xFF, 0xFF, 0xFF, 0xFF})
	if _, err := readFrame(r); err == nil {
		t.Fatal("expected oversized-frame error, got nil")
	}
}

func TestFramingCleanEOFReturnsEOF(t *testing.T) {
	r := bytes.NewReader(nil)
	_, err := readFrame(r)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("expected io.EOF, got %v", err)
	}
}
