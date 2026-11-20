package txgovernor

import (
	"context"
	"testing"
	"time"
)

func TestTimeSensitiveReportsExpireBeforeBusyChannelClears(t *testing.T) {
	for _, kind := range []string{"expired", "canceled", "ordinary"} {
		t.Run(kind, func(t *testing.T) {
			sender := &captureSender{}
			g := newDeterministic(sender)
			source := SubmitSource{Kind: "telemetry", Priority: PriorityBeacon}
			var deadline time.Time
			var canceled <-chan struct{}
			switch kind {
			case "expired":
				deadline = time.Now().Add(-time.Second)
			case "canceled":
				ch := make(chan struct{})
				close(ch)
				canceled = ch
			}
			if err := g.SubmitExpiring(context.Background(), 1, makeFrame(t, "test "+kind), source, deadline, canceled); err != nil {
				t.Fatal(err)
			}
			g.processOne(context.Background())
			want := 0
			if kind == "ordinary" {
				want = 1
			}
			if sender.Count() != want {
				t.Fatalf("sent=%d want=%d", sender.Count(), want)
			}
		})
	}
}
