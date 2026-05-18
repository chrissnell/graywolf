package modembridge

import (
	"log/slog"
	"sync"
	"time"
)

// pttWatchdog auto-unkeys any channel whose PTT has been held for longer
// than the configured timeout without a heartbeat. Each keyed:true call
// (initial or heartbeat) resets the timer; keyed:false cancels it.
//
// The watchdog lives entirely in Go so one process owns the timer — no
// cross-language coordination is required and the timer survives modem
// restarts cleanly.
type pttWatchdog struct {
	mu      sync.Mutex
	timers  map[uint32]*time.Timer
	timeout time.Duration
	unkey   func(channel uint32) error
	logger  *slog.Logger
}

func newPttWatchdog(timeout time.Duration, unkey func(uint32) error, logger *slog.Logger) *pttWatchdog {
	return &pttWatchdog{
		timers:  make(map[uint32]*time.Timer),
		timeout: timeout,
		unkey:   unkey,
		logger:  logger,
	}
}

// onKey starts (or resets) the watchdog timer for channel. Called on
// every keyed:true POST — both the initial press and the 2s heartbeats.
func (w *pttWatchdog) onKey(channel uint32) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if t, ok := w.timers[channel]; ok {
		t.Reset(w.timeout)
		return
	}
	w.timers[channel] = time.AfterFunc(w.timeout, func() {
		w.timerFire(channel)
	})
}

// onUnkey cancels the watchdog for channel. Called on explicit keyed:false POST.
func (w *pttWatchdog) onUnkey(channel uint32) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if t, ok := w.timers[channel]; ok {
		t.Stop()
		delete(w.timers, channel)
	}
}

// cancelAll stops and discards every active timer. Called on supervisor
// restart so stale timers from the previous modem session can't fire into
// a freshly-spawned child that has no keyed-PTT state.
func (w *pttWatchdog) cancelAll() {
	w.mu.Lock()
	defer w.mu.Unlock()
	for ch, t := range w.timers {
		t.Stop()
		delete(w.timers, ch)
	}
}

// timerFire is the AfterFunc callback. It sends an unkey and removes the
// timer entry. Errors from unkey are logged; the callback is fire-and-forget.
func (w *pttWatchdog) timerFire(channel uint32) {
	w.mu.Lock()
	delete(w.timers, channel)
	w.mu.Unlock()

	w.logger.Warn("ptt watchdog expired: auto-unkeying channel", "channel", channel)
	if err := w.unkey(channel); err != nil {
		w.logger.Error("ptt watchdog auto-unkey failed", "channel", channel, "err", err)
	}
}
