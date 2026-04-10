package app

import (
	"context"
	"log/slog"
	"time"

	"github.com/chrissnell/graywolf/pkg/aprs"
)

// aprsDropCounter is the subset of prometheus.Counter used by the submitter.
// Kept as a tiny interface so tests can substitute a simple counter without
// pulling in the full metrics registry.
type aprsDropCounter interface {
	Inc()
}

// aprsSubmitter owns the non-blocking submit path into the APRS fan-out
// queue. It is not safe for concurrent use: the caller (currently the
// modem->fan-out goroutine in main) is single-producer, so the rate-limited
// debug log uses a plain time.Time field rather than a mutex.
type aprsSubmitter struct {
	queue   chan<- *aprs.DecodedAPRSPacket
	dropped aprsDropCounter
	logger  *slog.Logger
	lastLog time.Time
}

func newAPRSSubmitter(q chan<- *aprs.DecodedAPRSPacket, c aprsDropCounter, l *slog.Logger) *aprsSubmitter {
	return &aprsSubmitter{queue: q, dropped: c, logger: l}
}

// submit enqueues pkt non-blockingly. On overflow, increments the drop
// counter once and emits a debug log at most every 10 seconds.
func (s *aprsSubmitter) submit(pkt *aprs.DecodedAPRSPacket) {
	select {
	case s.queue <- pkt:
		return
	default:
	}
	if s.dropped != nil {
		s.dropped.Inc()
	}
	now := time.Now()
	if s.logger != nil && now.Sub(s.lastLog) >= 10*time.Second {
		s.logger.Debug("aprs fan-out queue full, dropping packet", "queue_cap", cap(s.queue))
		s.lastLog = now
	}
}

// runAPRSFanOut consumes pkts from queue and forwards each to every non-nil
// output. Returns when queue is closed. Individual output errors are
// swallowed (outputs are expected to do their own logging), matching the
// existing behavior of the inlined loop this replaced.
func runAPRSFanOut(ctx context.Context, queue <-chan *aprs.DecodedAPRSPacket, outputs ...aprs.PacketOutput) {
	for pkt := range queue {
		for _, o := range outputs {
			if o == nil {
				continue
			}
			_ = o.SendPacket(ctx, pkt)
		}
	}
}
