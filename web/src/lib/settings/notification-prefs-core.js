// Pure logic for notification-prefs-store.svelte.js — kept runes-free so
// it can be unit-tested under `node --test` without a Svelte compile
// step (mirrors the channelsCore.js / releaseNotesCore.js split).

export const VALID_MODES = ['toast', 'os', 'both'];

/** @param {unknown} stored value read back from localStorage */
export function parseMode(stored) {
  return stored === 'os' || stored === 'both' ? stored : 'toast';
}

/**
 * @param {unknown} stored '1'/'0' from localStorage, or null if never set
 * @param {boolean} [def] value to use when never set; true for every
 *   existing per-type switch (on by default). stationNewEnabled passes
 *   false -- unlike a DM/bulletin/Emergency, a "new station heard" event
 *   can fire constantly on a busy APRS-IS-gated system, so it ships
 *   opt-in rather than joining the rest of the notifications that a
 *   fresh install already receives.
 */
export function parseEnabledFlag(stored, def = true) {
  return stored === null || stored === undefined ? def : stored === '1';
}

// How long a station must go unheard (from the timestamp this device
// last recorded it, not from a fixed reload/session boundary) before
// hearing it again counts as "new" and can raise a notification. Options
// span the operator's actual use case ("I heard them today, and I'd want
// a notification if I check again tomorrow") down to the tightest
// realistic value, plus a "Never" sentinel (stored as 0) that reproduces
// the old once-per-lifetime-per-callsign behavior -- see
// station-new-diff-core.js's thresholdMs = Infinity handling. Not
// exhaustive -- NotificationsSettings.svelte also offers a "Custom…"
// entry (see secsToCustomInput/customInputToSecs below) so the operator
// can type an exact hours/weeks value outside this list.
export const STATION_NEW_THRESHOLDS = [
  { value: 3600, label: '1 hour' },
  { value: 7200, label: '2 hours' },
  { value: 14400, label: '4 hours' },
  { value: 21600, label: '6 hours' },
  { value: 28800, label: '8 hours' },
  { value: 43200, label: '12 hours' },
  { value: 86400, label: '24 hours' },
  { value: 259200, label: '3 days' },
  { value: 604800, label: '1 week' },
  { value: 0, label: 'Never (once per call sign)' },
];

export const DEFAULT_STATION_NEW_THRESHOLD_SECS = 7200; // 2 hours

// Upper bound for a typed-in custom threshold (52 weeks) -- guards
// against a stray extra digit ("500" weeks) producing a value so large
// it's functionally "never" without saying so.
export const MAX_CUSTOM_THRESHOLD_SECS = 52 * 604800;

/**
 * @param {unknown} stored value read back from localStorage (seconds, as
 *   a string). Any non-negative integer is accepted, not just a
 *   STATION_NEW_THRESHOLDS value -- a typed-in custom threshold is
 *   stored the same way as a preset.
 */
export function parseStationNewThresholdSecs(stored) {
  const n = stored == null ? NaN : parseInt(stored, 10);
  if (!Number.isFinite(n) || n < 0) return DEFAULT_STATION_NEW_THRESHOLD_SECS;
  return Math.min(n, MAX_CUSTOM_THRESHOLD_SECS);
}

/** Converts the persisted seconds value to the ms threshold station-new-diff-core.js expects; 0 ("Never") becomes Infinity. */
export function stationNewThresholdMs(secs) {
  return secs === 0 ? Infinity : secs * 1000;
}

/** True when secs exactly matches one of STATION_NEW_THRESHOLDS' preset values (so the Select can show that preset instead of falling back to "Custom…"). */
export function isPresetThresholdSecs(secs) {
  return STATION_NEW_THRESHOLDS.some((o) => o.value === secs);
}

/**
 * Renders a threshold in seconds as a {count, unit} pair for the custom
 * hours/weeks input -- whole weeks when it divides evenly (and is at
 * least a week), otherwise whole hours (rounded, so an odd persisted
 * value like 90 minutes still shows something sane rather than 1.5).
 * @param {number} secs
 */
export function secsToCustomInput(secs) {
  if (secs > 0 && secs % 604800 === 0) return { count: secs / 604800, unit: 'weeks' };
  return { count: Math.max(1, Math.round(secs / 3600)), unit: 'hours' };
}

/**
 * Inverse of secsToCustomInput: a typed count + unit -> seconds, clamped
 * to [0, MAX_CUSTOM_THRESHOLD_SECS]. Non-numeric/negative input floors to 0.
 * @param {number|string} count
 * @param {'hours'|'weeks'} unit
 */
export function customInputToSecs(count, unit) {
  const n = Math.max(0, Math.floor(Number(count) || 0));
  const secs = unit === 'weeks' ? n * 604800 : n * 3600;
  return Math.min(secs, MAX_CUSTOM_THRESHOLD_SECS);
}

/**
 * Decide what mode to actually persist after requesting the browser's
 * Notification permission for 'os'/'both'. 'toast' never needs
 * permission. A denial (or anything other than 'granted') falls back to
 * 'toast' so the operator never lands silently in a dead mode.
 * @param {string} requestedMode
 * @param {string} permission Notification.permission after the prompt
 */
export function resolveModeAfterPermission(requestedMode, permission) {
  if (requestedMode !== 'os' && requestedMode !== 'both') return 'toast';
  return permission === 'granted' ? requestedMode : 'toast';
}
