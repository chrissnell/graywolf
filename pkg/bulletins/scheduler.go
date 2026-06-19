package bulletins

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

const (
	// BulletinInterval is the stable retransmit period for BLN0-9 per APRS spec.
	BulletinInterval = 20 * time.Minute
	// AnnouncementInterval is the retransmit period for BLNA-Z.
	AnnouncementInterval = 1 * time.Hour
	// BulletinMaxSends = 12 sends × 20 min ≈ 4 hours total coverage.
	BulletinMaxSends = 12
	// AnnouncementMaxSends = 96 sends × 1 hour = 4 days.
	AnnouncementMaxSends = 96

	// BulletinBurstCount is the number of rapid sends fired when a bulletin
	// is first created. The burst covers packet collisions and ensures the
	// bulletin is heard quickly even if the first transmission is lost.
	// After BulletinBurstCount sends the scheduler settles into BulletinInterval.
	// Per Bruninga/APRS protocol: new information should be retransmitted
	// rapidly at first, then decay to the Net Cycle Time.
	BulletinBurstCount = 3
	// BulletinBurstInterval is the gap between burst sends.
	BulletinBurstInterval = 30 * time.Second

	schedulerPollInterval = 15 * time.Second
)

// Scheduler periodically re-sends outbound bulletins per the APRS spec
// retransmit schedule. It wakes every minute and dispatches any row
// whose next_send_at is past.
type Scheduler struct {
	store     *Store
	sender    *Sender
	txChannel uint32
	logger    *slog.Logger

	kick chan struct{}
	done chan struct{}
	wg   sync.WaitGroup

	startOnce sync.Once
	stopOnce  sync.Once
}

// NewScheduler returns a ready Scheduler. Call Start to begin the loop.
func NewScheduler(store *Store, sender *Sender, txChannel uint32, logger *slog.Logger) *Scheduler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Scheduler{
		store:     store,
		sender:    sender,
		txChannel: txChannel,
		logger:    logger,
		kick:      make(chan struct{}, 1),
		done:      make(chan struct{}),
	}
}

// Start begins the retransmit loop. Idempotent.
func (sc *Scheduler) Start(ctx context.Context) {
	sc.startOnce.Do(func() {
		sc.wg.Add(1)
		go sc.loop(ctx)
	})
}

// Stop shuts down the loop and waits for it to exit. Idempotent.
func (sc *Scheduler) Stop() {
	sc.stopOnce.Do(func() { close(sc.done) })
	sc.wg.Wait()
}

// Kick wakes the scheduler immediately so a freshly-created bulletin is
// sent on the next tick without waiting up to a minute.
func (sc *Scheduler) Kick() {
	select {
	case sc.kick <- struct{}{}:
	default:
	}
}

func (sc *Scheduler) loop(ctx context.Context) {
	defer sc.wg.Done()
	ticker := time.NewTicker(schedulerPollInterval)
	defer ticker.Stop()
	// Send any due rows immediately on start.
	sc.processDue(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-sc.done:
			return
		case <-ticker.C:
			sc.processDue(ctx)
		case <-sc.kick:
			sc.processDue(ctx)
		}
	}
}

func (sc *Scheduler) processDue(ctx context.Context) {
	rows, err := sc.store.ListPendingSends(ctx, time.Now().UTC())
	if err != nil {
		sc.logger.Warn("bulletins scheduler list failed", "error", err)
		return
	}
	for i := range rows {
		b := &rows[i]
		if err := sc.sender.Send(ctx, b, sc.txChannel); err != nil {
			sc.logger.Warn("bulletins scheduler send failed",
				"id", b.ID, "slot", b.Slot, "error", err)
			continue
		}
		b.SendCount++
		if b.IsAnnouncement {
			next := time.Now().UTC().Add(AnnouncementInterval)
			b.NextSendAt = &next
		} else if b.SendCount < BulletinBurstCount {
			next := time.Now().UTC().Add(BulletinBurstInterval)
			b.NextSendAt = &next
		} else {
			if b.IntervalMins == 0 {
				// Burst-only: no more retransmits after the burst phase.
				b.NextSendAt = nil
			} else {
				next := time.Now().UTC().Add(time.Duration(b.IntervalMins) * time.Minute)
				b.NextSendAt = &next
			}
		}
		if err := sc.store.Update(ctx, b); err != nil {
			sc.logger.Warn("bulletins scheduler update failed",
				"id", b.ID, "slot", b.Slot, "error", err)
		}
	}
}
