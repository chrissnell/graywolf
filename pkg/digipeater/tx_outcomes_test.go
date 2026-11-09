package digipeater

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/app/ingress"
	"github.com/chrissnell/graywolf/pkg/ax25"
	pb "github.com/chrissnell/graywolf/pkg/ipcproto"
	"github.com/chrissnell/graywolf/pkg/txgovernor"
)

// Two different heard paths pass the digi's path-sensitive dedup check,
// but collide in the governor's path-independent dedup check (issue #679).
func TestDifferentPathsCountOneForwardedDigipeaterFrame(t *testing.T) {
	var forwarded, suppressed, legacy atomic.Int32
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	gov := txgovernor.New(txgovernor.Config{
		Logger:   logger,
		Sender:   func(*pb.TransmitFrame) error { return nil },
		Channels: map[uint32]txgovernor.ChannelTiming{1: {FullDup: true}},
		OnOutcome: func(_ uint32, source txgovernor.SubmitSource, outcome txgovernor.Outcome) {
			if source.Kind != "digipeater" {
				return
			}
			switch outcome {
			case txgovernor.OutcomeForwarded:
				forwarded.Add(1)
			case txgovernor.OutcomeDeduped:
				suppressed.Add(1)
			}
		},
	})
	digi, err := New(Config{
		MyCall: "N0CAL-1", Logger: logger, Submit: gov.Submit,
		Rules:    []Rule{{FromChannel: 1, ToChannel: 1, Alias: "WIDE", AliasType: "widen", MaxHops: 3, Action: "repeat"}},
		OnPacket: func(_ string, _, _ uint32, _ *ax25.Frame) { legacy.Add(1) },
	})
	if err != nil {
		t.Fatal(err)
	}
	digi.SetEnabled(true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); _ = gov.Run(ctx) }()
	defer func() { cancel(); <-done }()
	for _, path := range []string{"WIDE2-2", "WIDE2-1"} {
		if !digi.Handle(ctx, 1, buildFrame(t, "TEST1", "APRS", []string{path}, "same payload"), ingress.Modem()) {
			t.Fatal("expected submission handled")
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for forwarded.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	cancel()
	<-done
	if legacy.Load() != 2 || forwarded.Load() != 1 || suppressed.Load() != 1 {
		t.Fatalf("legacy=%d forwarded=%d suppressed=%d", legacy.Load(), forwarded.Load(), suppressed.Load())
	}
	text := logs.String()
	for message, want := range map[string]int{"msg=\"digipeater submission handled\"": 2, "msg=\"tx deduped\"": 1, "msg=\"tx forwarded\"": 1} {
		if got := strings.Count(text, message); got != want {
			t.Errorf("%s: got %d, want %d; logs=%s", message, got, want, text)
		}
	}
	if strings.Contains(text, "msg=digipeated") || strings.Contains(text, "msg=\"digipeated\"") {
		t.Error("misleading digipeated event remains")
	}
}
