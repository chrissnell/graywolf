package tncprofile

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

func restartTestPort(t *testing.T) (*restartPort, net.Conn) {
	t.Helper()
	port, radio := net.Pipe()
	p := newRestartPort(port, bufio.NewReader(port))
	t.Cleanup(func() {
		_ = p.Close()
		_ = radio.Close()
		select {
		case <-p.readerDone:
		case <-time.After(time.Second):
			t.Error("serial reader leaked after Close")
		}
	})
	return p, radio
}

func TestRestartRejectsFragmentedNegativeReplies(t *testing.T) {
	for _, reply := range []string{"?EH\rcmd:", "?EHcmd:", "ERROR\r", "N\n"} {
		t.Run(reply, func(t *testing.T) {
			port, radio := restartTestPort(t)
			go func() {
				for _, b := range []byte(reply) {
					if _, err := radio.Write([]byte{b}); err != nil {
						return
					}
				}
			}()
			err := port.waitRestart(context.Background(), time.Second)
			if err == nil || !strings.Contains(err.Error(), "rejected RESTART") {
				t.Fatalf("accepted failed restart: %v", err)
			}
		})
	}
}

func TestRestartNeedsNoReplyAndKeepsReaderAlive(t *testing.T) {
	port, radio := restartTestPort(t)
	if err := port.waitRestart(context.Background(), 10*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	go func() { _, _ = radio.Write([]byte{0xc0, 0x00, 0x42, 0xc0}) }()
	got := make([]byte, 4)
	if _, err := io.ReadFull(port, got); err != nil || string(got) != "\xc0\x00\x42\xc0" {
		t.Fatalf("post-restart frame: %x, %v", got, err)
	}
}

func TestRestartPreservesStartupFramesAndIgnoresPayloadText(t *testing.T) {
	port, radio := restartTestPort(t)
	// An APRS payload can contain strings that look like TNC error replies.
	want := "banner\r\xc0\x00?EH\rERRORcmd:\xc0\xc0\x00second\xc0"
	go func() {
		for _, b := range []byte(want) {
			if _, err := radio.Write([]byte{b}); err != nil {
				return
			}
		}
	}()
	if err := port.waitRestart(context.Background(), 30*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, len(want))
	if _, err := io.ReadFull(port, got); err != nil || string(got) != want {
		t.Fatalf("startup bytes changed: %x, %v", got, err)
	}
}

func TestRestartDisconnectAndCancellation(t *testing.T) {
	t.Run("disconnect", func(t *testing.T) {
		port, radio := restartTestPort(t)
		_ = radio.Close()
		if err := port.waitRestart(context.Background(), time.Second); !errors.Is(err, io.EOF) {
			t.Fatalf("disconnect ignored: %v", err)
		}
	})
	t.Run("cancel", func(t *testing.T) {
		port, _ := restartTestPort(t)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel()
		if err := port.waitRestart(ctx, time.Second); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("cancellation ignored: %v", err)
		}
	})
}

func TestRestartStartupBufferBound(t *testing.T) {
	port, radio := restartTestPort(t)
	data := bytes.Repeat([]byte{0x42}, 256*1024+1024)
	data[0] = 0xc0 // binary KISS traffic, not a long command response
	go func() { _, _ = radio.Write(data) }()
	if err := port.waitRestart(context.Background(), time.Second); err == nil || !strings.Contains(err.Error(), "256 KiB") {
		t.Fatalf("unbounded startup buffer: %v", err)
	}
}
