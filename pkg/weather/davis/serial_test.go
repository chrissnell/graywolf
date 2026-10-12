package davis

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"
)

type replayPort struct {
	rx     *bytes.Reader
	tx     bytes.Buffer
	closed bool
}

func (p *replayPort) Read(b []byte) (int, error) {
	n, err := p.rx.Read(b)
	if err == io.EOF {
		return 0, nil
	}
	return n, err
}
func (p *replayPort) Write(b []byte) (int, error)        { return p.tx.Write(b) }
func (p *replayPort) Close() error                       { p.closed = true; return nil }
func (p *replayPort) SetReadTimeout(time.Duration) error { return nil }

func TestExchangeMatchesDavisSimulatorConversation(t *testing.T) {
	rx := append([]byte{'\n', '\r', 0x06}, testPacket(PacketLOOP)...)
	rx = append(rx, testPacket(PacketLOOP2)...)
	port := &replayPort{rx: bytes.NewReader(rx)}
	got, err := Exchange(context.Background(), port, Bucket02MM)
	if err != nil {
		t.Fatal(err)
	}
	if port.tx.String() != "\nLPS 3 2\n" {
		t.Fatalf("serial writes = %q", port.tx.String())
	}
	if !port.closed || got.GustKPH == nil || got.RainMonthMM == nil {
		t.Fatalf("incomplete exchange: closed=%v observation=%+v", port.closed, got)
	}
}
