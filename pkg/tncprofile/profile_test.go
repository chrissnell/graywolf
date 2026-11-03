package tncprofile

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

func TestKenwoodHandshakeAndBufferedKISS(t *testing.T) {
	for _, initialMode := range []string{"command", "kiss"} {
		t.Run(initialMode, func(t *testing.T) {
			port, radio := net.Pipe()
			defer port.Close()
			frame := []byte{0xc0, 0x00, 0x42, 0xc0}
			done := make(chan error, 1)
			go func() {
				defer radio.Close()
				for _, step := range []struct{ request, response string }{
					{"\x03\xc0\xff\xc0\x03\r", "\rcmd:"},
					{"KISS ON\r", "\rKISS was OFF\rcmd:" + string(frame)},
					{"RESTART\r", ""},
				} {
					data := make([]byte, len(step.request))
					if _, err := io.ReadFull(radio, data); err != nil {
						done <- err
						return
					}
					if string(data) != step.request {
						done <- errors.New("unexpected initialization bytes")
						return
					}
					if step.response != "" {
						if _, err := io.WriteString(radio, step.response); err != nil {
							done <- err
							return
						}
					}
				}
				done <- nil
			}()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			initialized, err := initialize(ctx, port, Kenwood, 0)
			if err != nil {
				t.Fatal(err)
			}
			got, err := io.ReadAll(initialized)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, frame) {
				t.Fatalf("buffered KISS bytes lost: %x", got)
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestKenwoodInitializationCancellationUnblocksRead(t *testing.T) {
	port, radio := net.Pipe()
	defer radio.Close()
	defer port.Close()
	go func() { _, _ = io.Copy(io.Discard, radio) }() // accepts commands, never replies
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	_, err := initialize(ctx, port, Kenwood, 0)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected bounded timeout, got %v", err)
	}
}

func TestNativeInitializationLeavesTransportUntouched(t *testing.T) {
	port, radio := net.Pipe()
	defer radio.Close()
	defer port.Close()
	got, err := Initialize(context.Background(), port, Native)
	if err != nil || got != port {
		t.Fatalf("native transport modified: %v", err)
	}
}

func TestUnknownProfileRejected(t *testing.T) {
	port, radio := net.Pipe()
	defer radio.Close()
	defer port.Close()
	if _, err := Initialize(context.Background(), port, "unknown"); err == nil {
		t.Fatal("expected error")
	}
}
