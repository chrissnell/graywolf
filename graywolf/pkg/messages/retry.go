package messages

import (
	"context"
	"errors"
	"log/slog"
	"math/rand"
	"sync"
	"time"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/txgovernor"
)

// RetryBackoff is the default DM ack-timeout backoff ladder. Each
// entry is a target delay for the Nth attempt; ±10% jitter is applied
// at scheduling time. When attempts exceed the ladder length the
// final value is reused until the preferences.RetryMaxAttempts cap
// fires.
var RetryBackoff = []time.Duration{
	30 * time.Second,
	60 * time.Second,
	120 * time.Second,
	300 * time.Second,
	600 * time.Second,
}

// DefaultRetryMaxAttempts is the cap applied when MessagePreferences
// has an unset (0) value. Matches the seeded default.
const DefaultRetryMaxAttempts = 5

// RetryManagerConfig captures the retry loop's collaborators. All
// fields except Logger, Clock, and Rand are required.
type RetryManagerConfig struct {
	Store       *Store
	Sender      *Sender
	Preferences *Preferences
	EventHub    *EventHub
	Logger      *slog.Logger
	Clock       SenderClock
	// Rand is the jitter source. If nil, a time-seeded *rand.Rand is
	// used. Tests inject a deterministic source.
	Rand *rand.Rand
}

// RetryManager owns the single goroutine that wakes on timer expiry
// or explicit kick() calls to scan ListRetryDue and re-submit DM
// outbound via Sender.Send. Tactical rows never participate — they
// go terminal on first send (see Sender.onTxComplete).
type RetryManager struct {
	cfg RetryManagerConfig

	logger *slog.Logger
	clock  SenderClock
	rng    *rand.Rand
	rngMu  sync.Mutex

	// kick is the wake-up channel. Non-blocking sends — a coalesced
	// burst of kicks produces one iteration.
	kick chan struct{}

	// inFlight guards concurrent submits for the same row id. The
	// retry loop and /resend both consult and mutate this map so a
	// second submit is a no-op until the first returns.
	inFlightMu sync.Mutex
	inFlight   map[uint64]struct{}

	// lifecycle
	startOnce sync.Once
	stopOnce  sync.Once
	done      chan struct{}
	cancel    context.CancelFunc
	wg        sync.WaitGroup
}

// NewRetryManager validates cfg and returns a ready manager.
// Lifecycle: call Start(ctx) once, Stop() to shut down.
func NewRetryManager(cfg RetryManagerConfig) (*RetryManager, error) {
	if cfg.Store == nil {
		return nil, errors.New("messages: RetryManager requires Store")
	}
	if cfg.Sender == nil {
		return nil, errors.New("messages: RetryManager requires Sender")
	}
	if cfg.Preferences == nil {
		return nil, errors.New("messages: RetryManager requires Preferences")
	}
	if cfg.EventHub == nil {
		return nil, errors.New("messages: RetryManager requires EventHub")
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	clock := cfg.Clock
	if clock == nil {
		clock = realRouterClock{}
	}
	rng := cfg.Rand
	if rng == nil {
		rng = rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	return &RetryManager{
		cfg:      cfg,
		logger:   logger,
		clock:    clock,
		rng:      rng,
		kick:     make(chan struct{}, 1),
		inFlight: make(map[uint64]struct{}),
		done:     make(chan struct{}),
	}, nil
}

// Start spins up the retry goroutine and bootstraps from
// ListAwaitingAckOnStartup — already-enrolled DM rows resume.
// Idempotent.
func (r *RetryManager) Start(ctx context.Context) {
	r.startOnce.Do(func() {
		runCtx, cancel := context.WithCancel(ctx)
		r.cancel = cancel
		r.wg.Add(1)
		go r.loop(runCtx)
		// Bootstrap: enroll any pre-existing DM awaiting-ack rows. A
		// row without NextRetryAt (e.g. a fresh insert that never
		// got scheduled) is arm-by-Kick on the first iteration. A
		// row with NextRetryAt in the past triggers immediately.
		r.bootstrap(runCtx)
	})
}

// Stop cancels the goroutine and waits for it to exit. Idempotent.
func (r *RetryManager) Stop() {
	r.stopOnce.Do(func() {
		if r.cancel != nil {
			r.cancel()
		}
		close(r.done)
	})
	r.wg.Wait()
}

// Kick wakes the retry goroutine so it re-scans ListRetryDue. Safe
// to call from any goroutine; non-blocking.
func (r *RetryManager) Kick() {
	select {
	case r.kick <- struct{}{}:
	default:
	}
}

// Resend re-submits the row identified by id via Sender.Send. Used
// by the REST /resend handler. Resets the row's attempt counter and
// clears FailureReason/NextRetryAt; for tactical rows, submits once
// without re-enrolling in the retry ladder.
//
// Returns SendResult so the handler can surface the outcome. An
// in-flight guard prevents the retry loop from double-submitting
// concurrently.
func (r *RetryManager) Resend(ctx context.Context, id uint64) (SendResult, error) {
	if !r.acquireInFlight(id) {
		return SendResult{}, errors.New("messages: resend already in flight")
	}
	defer r.releaseInFlight(id)
	row, err := r.cfg.Store.GetByID(ctx, id)
	if err != nil {
		return SendResult{}, err
	}
	if row.Direction != "out" {
		return SendResult{}, errors.New("messages: resend requires outbound row")
	}
	// Reset attempt state for a fresh operator-triggered send.
	row.Attempts = 0
	row.NextRetryAt = nil
	row.FailureReason = ""
	// Don't clear SentAt/AckedAt — callers may /resend a failed
	// outbound that was never accepted; SentAt stays nil on that
	// path, and the TxHook will flip it when the new submit fires.
	if err := r.cfg.Store.Update(ctx, row); err != nil {
		return SendResult{}, err
	}
	result := r.cfg.Sender.Send(ctx, row)
	if result.Err == nil && result.Retryable && row.ThreadKind == ThreadKindDM {
		// Re-enroll: schedule the next attempt per the backoff.
		r.scheduleNext(ctx, row)
	}
	return result, nil
}

// CancelRetry clears NextRetryAt and removes the row from the
// in-flight map. Called by the REST soft-delete handler before
// store.SoftDelete so a concurrent retry loop does not race.
func (r *RetryManager) CancelRetry(ctx context.Context, id uint64) error {
	r.inFlightMu.Lock()
	delete(r.inFlight, id)
	r.inFlightMu.Unlock()
	row, err := r.cfg.Store.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if row.NextRetryAt == nil {
		return nil
	}
	row.NextRetryAt = nil
	return r.cfg.Store.Update(ctx, row)
}

// loop is the retry goroutine's main function. Sleeps until the
// soonest NextRetryAt (or 60s if no rows are due) or a kick.
func (r *RetryManager) loop(ctx context.Context) {
	defer r.wg.Done()

	timer := time.NewTimer(r.nextWake(ctx))
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		case <-r.kick:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		}
		r.processDue(ctx)
		timer.Reset(r.nextWake(ctx))
	}
}

// nextWake returns the duration until the next NextRetryAt, capped
// at 60s so a stuck timer wakes regularly. An empty due-set returns
// the 60s idle tick.
func (r *RetryManager) nextWake(ctx context.Context) time.Duration {
	const idleTick = 60 * time.Second
	rows, err := r.cfg.Store.ListAwaitingAckOnStartup(ctx)
	if err != nil {
		return idleTick
	}
	now := r.clock.Now()
	var soonest time.Duration
	first := true
	for _, row := range rows {
		if row.NextRetryAt == nil {
			continue
		}
		d := row.NextRetryAt.Sub(now)
		if d < 0 {
			return 0
		}
		if first || d < soonest {
			soonest = d
			first = false
		}
	}
	if first {
		return idleTick
	}
	if soonest > idleTick {
		return idleTick
	}
	return soonest
}

// processDue runs one iteration: scans ListRetryDue, re-submits
// each row, and records the outcome.
func (r *RetryManager) processDue(ctx context.Context) {
	now := r.clock.Now()
	rows, err := r.cfg.Store.ListRetryDue(ctx, now)
	if err != nil {
		r.logger.Warn("messages retry list failed", "error", err)
		return
	}
	for _, row := range rows {
		r.retryOne(ctx, row)
	}
}

// retryOne handles one retry attempt.
func (r *RetryManager) retryOne(ctx context.Context, row configstore.Message) {
	if !r.acquireInFlight(row.ID) {
		return
	}
	defer r.releaseInFlight(row.ID)
	// Re-read the row so we pick up any racy updates (soft-delete,
	// ack arrival) between ListRetryDue and this call.
	cur, err := r.cfg.Store.GetByID(ctx, row.ID)
	if err != nil {
		r.logger.Warn("messages retry reload failed", "error", err, "id", row.ID)
		return
	}
	if cur.AckState != AckStateNone {
		// Already closed (acked / rejected / broadcast). Clear the
		// schedule if still set and move on.
		if cur.NextRetryAt != nil {
			cur.NextRetryAt = nil
			_ = r.cfg.Store.Update(ctx, cur)
		}
		return
	}
	if cur.DeletedAt.Valid {
		// Soft-deleted — drop the schedule to be tidy. GetByID normally
		// excludes soft-deleted rows, but be defensive.
		cur.NextRetryAt = nil
		_ = r.cfg.Store.Update(ctx, cur)
		return
	}
	maxAttempts := r.maxAttempts()
	if cur.Attempts >= maxAttempts {
		r.fail(ctx, cur, "retry budget exhausted")
		return
	}
	cur.Attempts++
	result := r.cfg.Sender.Send(ctx, cur)
	if result.Err == nil {
		// Submit accepted — schedule the next ack timeout.
		r.scheduleNext(ctx, cur)
		return
	}
	// ErrQueueFull: short retry, does NOT count against budget.
	if errors.Is(result.Err, txgovernor.ErrQueueFull) {
		cur.Attempts-- // roll back the increment
		cur.NextRetryAt = timePtr(r.clock.Now().Add(ShortRetryDelay))
		if err := r.cfg.Store.Update(ctx, cur); err != nil {
			r.logger.Warn("messages retry short-retry update failed", "error", err, "id", cur.ID)
		}
		return
	}
	// ErrStopped: governor is shutting down; don't fail the row,
	// just leave it enrolled. The service restart will re-attempt.
	if errors.Is(result.Err, txgovernor.ErrStopped) {
		cur.Attempts-- // roll back so the budget survives restart
		_ = r.cfg.Store.Update(ctx, cur)
		return
	}
	// Any other error — treat as a failed attempt. If the budget
	// is now exhausted, fail; otherwise schedule the next attempt.
	if cur.Attempts >= maxAttempts {
		r.fail(ctx, cur, "send error: "+result.Err.Error())
		return
	}
	r.scheduleNext(ctx, cur)
}

// fail marks row terminally failed and emits a failed event.
func (r *RetryManager) fail(ctx context.Context, row *configstore.Message, reason string) {
	row.FailureReason = truncReason(reason)
	row.NextRetryAt = nil
	row.AckState = AckStateRejected
	now := r.clock.Now()
	row.AckedAt = timePtr(now)
	if err := r.cfg.Store.Update(ctx, row); err != nil {
		r.logger.Warn("messages retry fail update failed", "error", err, "id", row.ID)
		return
	}
	r.cfg.EventHub.Publish(Event{
		Type:       EventMessageFailed,
		MessageID:  row.ID,
		ThreadKind: row.ThreadKind,
		ThreadKey:  row.ThreadKey,
		Timestamp:  now,
	})
}

// scheduleNext computes the next NextRetryAt per the backoff ladder
// and persists it.
func (r *RetryManager) scheduleNext(ctx context.Context, row *configstore.Message) {
	delay := r.backoffFor(int(row.Attempts))
	row.NextRetryAt = timePtr(r.clock.Now().Add(delay))
	if err := r.cfg.Store.Update(ctx, row); err != nil {
		r.logger.Warn("messages retry schedule update failed", "error", err, "id", row.ID)
		return
	}
	// Nudge the loop so it picks up the new schedule.
	r.Kick()
}

// backoffFor returns the target delay for the nth attempt (1-indexed),
// with ±10% multiplicative jitter. Attempts beyond the ladder reuse
// the last entry.
func (r *RetryManager) backoffFor(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	idx := attempt - 1
	if idx >= len(RetryBackoff) {
		idx = len(RetryBackoff) - 1
	}
	base := RetryBackoff[idx]
	// ±10% multiplicative jitter. rng is not safe for concurrent
	// use, so guard under rngMu.
	r.rngMu.Lock()
	jitter := 0.9 + 0.2*r.rng.Float64()
	r.rngMu.Unlock()
	return time.Duration(float64(base) * jitter)
}

// maxAttempts returns the current retry cap from preferences.
func (r *RetryManager) maxAttempts() uint32 {
	p := r.cfg.Preferences.Current()
	if p == nil || p.RetryMaxAttempts == 0 {
		return DefaultRetryMaxAttempts
	}
	return p.RetryMaxAttempts
}

// acquireInFlight reserves id in the in-flight map. Returns false if
// id is already in flight (another Resend/retry is running).
func (r *RetryManager) acquireInFlight(id uint64) bool {
	r.inFlightMu.Lock()
	defer r.inFlightMu.Unlock()
	if _, ok := r.inFlight[id]; ok {
		return false
	}
	r.inFlight[id] = struct{}{}
	return true
}

// releaseInFlight drops id from the in-flight map.
func (r *RetryManager) releaseInFlight(id uint64) {
	r.inFlightMu.Lock()
	delete(r.inFlight, id)
	r.inFlightMu.Unlock()
}

// bootstrap enrolls any DM awaiting-ack rows on startup so the retry
// goroutine picks them up from where they left off.
func (r *RetryManager) bootstrap(ctx context.Context) {
	rows, err := r.cfg.Store.ListAwaitingAckOnStartup(ctx)
	if err != nil {
		r.logger.Warn("messages retry bootstrap failed", "error", err)
		return
	}
	if len(rows) > 0 {
		// Kick the loop so it processes any rows whose NextRetryAt is
		// already past (e.g. a graceful restart after a long sleep).
		r.Kick()
	}
}

// timePtr returns a *time.Time for t, because gorm requires a pointer
// for nullable time columns.
func timePtr(t time.Time) *time.Time {
	return &t
}
