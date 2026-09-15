// Pure load/cap logic for the device-local notifications log --
// persisted history of every popup/OS notification actually raised on
// this device (message, bulletin, station emergency, new station,
// favorite), shown on the Notifications Log page (sidebar, between APRS
// Logs and System Logs). Device-local like the rest of the notification
// prefs (mode, sounds, threshold) -- each browser tab/device already
// makes its own independent decision about whether to notify (muted
// threads, active thread, map filters), so a per-device log reflects
// what THIS device actually showed, matching how firing already works
// rather than requiring new backend infrastructure to unify it.

export const MAX_LOG_ENTRIES = 200;

/**
 * Parse a persisted JSON array of log entries. Corrupt/missing input, or
 * an individual malformed entry, is dropped rather than throwing.
 * @param {string | null} raw
 * @returns {Array<{id: string, kind: string, title: string, body: string, href: string, timestamp: number}>}
 */
export function parseLogEntries(raw) {
  if (!raw) return [];
  let arr;
  try {
    arr = JSON.parse(raw);
  } catch {
    return [];
  }
  if (!Array.isArray(arr)) return [];
  return arr.filter(
    (e) =>
      e &&
      typeof e.id === 'string' &&
      typeof e.kind === 'string' &&
      typeof e.title === 'string' &&
      typeof e.timestamp === 'number' &&
      Number.isFinite(e.timestamp),
  );
}

/**
 * Prepend a new entry (list is kept newest-first) and cap at
 * MAX_LOG_ENTRIES, dropping the oldest. Pure -- does not mutate `entries`.
 * @param {Array<object>} entries
 * @param {object} entry
 * @returns {Array<object>}
 */
export function addLogEntry(entries, entry) {
  const next = [entry, ...(entries || [])];
  return next.length > MAX_LOG_ENTRIES ? next.slice(0, MAX_LOG_ENTRIES) : next;
}
