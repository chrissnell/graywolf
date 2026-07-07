// Device-local AX.25 Terminal "Advanced timing & windowing" settings.
// Like the log/ui-scale prefs, these are intentionally NOT synced to the
// server: retry count (N2), the T1/T2/T3 timers, paclen, window size,
// modulo, and backoff are per-device tuning an operator dials in once for
// their radio + TNC and expects to persist across sessions rather than
// resetting to the server defaults on every new connect (graywolf #456).
//
// Persisted as one JSON blob so adding a knob later doesn't strand old
// keys. Unknown / malformed storage falls back to defaults field by
// field, so a partial or corrupt entry never breaks the form.

const LS_KEY = 'ax25-terminal-advanced';

export const advancedDefaults = Object.freeze({
  mod128: false,
  paclen: 0,
  maxframe: 0,
  n2: 0,
  t1ms: 0,
  t2ms: 0,
  t3ms: 0,
  backoff: 'linear',
});

// loadAdvanced returns the persisted settings merged over the defaults.
// Never throws: a missing key, disabled storage, or bad JSON yields the
// defaults.
export function loadAdvanced() {
  try {
    const raw = localStorage.getItem(LS_KEY);
    if (!raw) return { ...advancedDefaults };
    const parsed = JSON.parse(raw);
    if (!parsed || typeof parsed !== 'object') return { ...advancedDefaults };
    return {
      mod128: typeof parsed.mod128 === 'boolean' ? parsed.mod128 : advancedDefaults.mod128,
      paclen: Number.isFinite(parsed.paclen) ? parsed.paclen : advancedDefaults.paclen,
      maxframe: Number.isFinite(parsed.maxframe) ? parsed.maxframe : advancedDefaults.maxframe,
      n2: Number.isFinite(parsed.n2) ? parsed.n2 : advancedDefaults.n2,
      t1ms: Number.isFinite(parsed.t1ms) ? parsed.t1ms : advancedDefaults.t1ms,
      t2ms: Number.isFinite(parsed.t2ms) ? parsed.t2ms : advancedDefaults.t2ms,
      t3ms: Number.isFinite(parsed.t3ms) ? parsed.t3ms : advancedDefaults.t3ms,
      backoff: typeof parsed.backoff === 'string' ? parsed.backoff : advancedDefaults.backoff,
    };
  } catch {
    return { ...advancedDefaults };
  }
}

// saveAdvanced persists the current settings. Best-effort: storage errors
// (private mode, quota) are swallowed so a connect never fails on a
// preference write.
export function saveAdvanced(s) {
  try {
    localStorage.setItem(
      LS_KEY,
      JSON.stringify({
        mod128: !!s.mod128,
        paclen: Number(s.paclen) || 0,
        maxframe: Number(s.maxframe) || 0,
        n2: Number(s.n2) || 0,
        t1ms: Number(s.t1ms) || 0,
        t2ms: Number(s.t2ms) || 0,
        t3ms: Number(s.t3ms) || 0,
        backoff: s.backoff || 'linear',
      })
    );
  } catch {
    // Non-fatal: settings just won't persist on this device.
  }
}
