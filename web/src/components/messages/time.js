// Small relative-time + day-header formatting helpers used by the
// Messages components. All times are ISO strings from the API.

const MINUTE = 60;
const HOUR = 60 * 60;
const DAY = 24 * HOUR;
const WEEK = 7 * DAY;

/** "now", "5m", "2h", "3d", or absolute date for older. */
export function relativeShort(iso) {
  if (!iso) return '';
  const t = Date.parse(iso);
  if (Number.isNaN(t)) return '';
  const secs = Math.max(0, Math.floor((Date.now() - t) / 1000));
  if (secs < 30) return 'now';
  if (secs < MINUTE) return `${secs}s`;
  if (secs < HOUR) return `${Math.floor(secs / MINUTE)}m`;
  if (secs < DAY) return `${Math.floor(secs / HOUR)}h`;
  if (secs < WEEK) return `${Math.floor(secs / DAY)}d`;
  const d = new Date(t);
  return d.toLocaleDateString(undefined, { month: 'short', day: 'numeric' });
}

/** "3 min ago", "2 hr ago", "yesterday", absolute else. */
export function relativeLong(iso) {
  if (!iso) return '';
  const t = Date.parse(iso);
  if (Number.isNaN(t)) return '';
  const secs = Math.max(0, Math.floor((Date.now() - t) / 1000));
  if (secs < 30) return 'just now';
  if (secs < MINUTE) return `${secs} sec ago`;
  if (secs < HOUR) return `${Math.floor(secs / MINUTE)} min ago`;
  if (secs < DAY) return `${Math.floor(secs / HOUR)} hr ago`;
  if (secs < 2 * DAY) return 'yesterday';
  if (secs < WEEK) return `${Math.floor(secs / DAY)} days ago`;
  return new Date(t).toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' });
}

/** HH:MM in local time, 24h, no seconds. */
export function timeOfDay(iso) {
  if (!iso) return '';
  const t = Date.parse(iso);
  if (Number.isNaN(t)) return '';
  const d = new Date(t);
  const h = String(d.getHours()).padStart(2, '0');
  const m = String(d.getMinutes()).padStart(2, '0');
  return `${h}:${m}`;
}

/** "Today", "Yesterday", or "Mon, Apr 14" for day-separator headers. */
export function dayHeader(iso) {
  if (!iso) return '';
  const t = Date.parse(iso);
  if (Number.isNaN(t)) return '';
  const d = new Date(t);
  const now = new Date();
  const midnight = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime();
  const msgMidnight = new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();
  const diff = Math.round((midnight - msgMidnight) / (DAY * 1000));
  if (diff === 0) return 'Today';
  if (diff === 1) return 'Yesterday';
  const sameYear = d.getFullYear() === now.getFullYear();
  return d.toLocaleDateString(undefined, {
    weekday: 'short',
    month: 'short',
    day: 'numeric',
    year: sameYear ? undefined : 'numeric',
  });
}

/** Returns the yyyy-mm-dd key used to detect day-separator transitions. */
export function dayKey(iso) {
  if (!iso) return '';
  const t = Date.parse(iso);
  if (Number.isNaN(t)) return '';
  const d = new Date(t);
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
}
