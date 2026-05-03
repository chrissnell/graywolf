// Compact relative-time formatter shared across the actions tables.
// Returns "—" for null/undefined and unparseable input. Logs a warning
// on parse failures so QA notices backend timestamp drift.
export function timeAgo(isoStr) {
  if (!isoStr) return '—';
  const ms = Date.now() - new Date(isoStr).getTime();
  if (Number.isNaN(ms)) {
    console.warn('timeAgo: unparseable timestamp', isoStr);
    return '—';
  }
  const sec = Math.floor(ms / 1000);
  if (sec < 60) return `${sec}s ago`;
  const min = Math.floor(sec / 60);
  if (min < 60) return `${min} min ago`;
  const hr = Math.floor(min / 60);
  if (hr < 24) return `${hr}h ${min % 60}m ago`;
  const day = Math.floor(hr / 24);
  return `${day}d ago`;
}
