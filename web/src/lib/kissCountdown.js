// Countdown text helpers for KISS tcp-client supervisor state (Phase 4).
// Kept in a pure-JS module so node --test can exercise the bucket
// logic without dragging in Svelte.
//
// The server hands us `retry_at_unix_ms` (wall-clock); the client
// subtracts Date.now() to compute remaining delay, then formats per
// D16's coarse/fine rule:
//
//   > 30s: "Reconnecting in ~Nm" rounded UP to the nearest minute.
//   ≤ 30s: "Reconnecting in Ns" precise.
//   ≤ 0  : "Reconnecting now…" (fencepost during the transition).

export const MS_PER_SECOND = 1000;
export const COARSE_BUCKET_MS = 30 * 1000;

/**
 * Compute the reconnect-countdown text for a tcp-client supervisor.
 *
 * @param {number|undefined|null} retryAtUnixMs Wall-clock deadline (ms).
 * @param {number} nowMs Current wall-clock time (ms). Defaults to Date.now().
 * @returns {string} Human-readable countdown, or empty string when
 *   retryAtUnixMs is 0/null/undefined (no pending retry).
 */
export function countdownText(retryAtUnixMs, nowMs = Date.now()) {
  if (!retryAtUnixMs) return '';
  const remainMs = retryAtUnixMs - nowMs;
  if (remainMs <= 0) {
    return 'Reconnecting now…';
  }
  if (remainMs <= COARSE_BUCKET_MS) {
    const secs = Math.ceil(remainMs / MS_PER_SECOND);
    return `Reconnecting in ${secs}s`;
  }
  // Coarse bucket: round UP to the next whole minute. 31000ms → "~1m";
  // 90000ms → "~2m"; 3 * 60 * 1000 + 500 → "~4m".
  const mins = Math.ceil(remainMs / (60 * MS_PER_SECOND));
  return `Reconnecting in ~${mins}m`;
}

/**
 * Return a short state pill label for a kiss interface. Mirrors the
 * backend's State* constants from pkg/kiss/manager.go.
 */
export function stateLabel(state) {
  switch (state) {
    case 'connected':   return 'Connected';
    case 'connecting':  return 'Connecting…';
    case 'backoff':     return 'Reconnecting';
    case 'disconnected': return 'Disconnected';
    case 'listening':   return 'Listening';
    case 'stopped':     return 'Stopped';
    default:            return state || 'Unknown';
  }
}

/**
 * Return the badge variant color for a state. Keeps the Kiss page and
 * any other surface consistent. Uses chonky-ui Badge variants:
 * success / info / warning / error.
 */
export function stateBadgeVariant(state) {
  switch (state) {
    case 'connected':
    case 'listening':
      return 'success';
    case 'connecting':
      return 'info';
    case 'backoff':
    case 'disconnected':
      return 'warning';
    case 'stopped':
      return 'error';
    default:
      return 'info';
  }
}

/**
 * Format a Unix millisecond timestamp as "HH:MM:SS" in local time.
 * Used by the status detail row to show "Connected since…" without a
 * date (operator is looking at a live system, a date is noise). 0
 * returns ''.
 */
export function formatLocalTime(unixMs) {
  if (!unixMs) return '';
  const d = new Date(unixMs);
  return d.toLocaleTimeString();
}

/**
 * Whether the Retry-now button should be visible. Only when the
 * supervisor is NOT actively connected — during connecting we let
 * the existing dial attempt finish.
 */
export function canRetryNow(state) {
  return state === 'backoff' || state === 'disconnected';
}
