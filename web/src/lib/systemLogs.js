// Pure helpers for the System Logs tab. Kept framework-free so they can
// be unit-tested with `node --test` (the repo's frontend test runner).

// formatAttrValue renders one structured-attribute value the way slog's
// text handler does: bare when safe, double-quoted when the string form
// is empty or contains whitespace, '=', or a quote. Objects/arrays are
// JSON-encoded first. null/undefined render as the slog sentinel <nil>.
export function formatAttrValue(v) {
  if (v === null || v === undefined) return '<nil>';
  const s = typeof v === 'object' ? JSON.stringify(v) : String(v);
  return s === '' || /[\s"=]/.test(s) ? JSON.stringify(s) : s;
}

// formatAttrs renders an entry's structured attributes as a single
// space-separated "key=value" run, matching the daemon's console/journal
// output. Returns '' when there are no attributes.
export function formatAttrs(attrs) {
  if (!attrs || typeof attrs !== 'object') return '';
  const parts = Object.entries(attrs).map(([k, v]) => `${k}=${formatAttrValue(v)}`);
  return parts.join(' ');
}

// formatLogsForClipboard renders log entries as plain text, one line per
// entry: "<timestamp> <LEVEL> [component] <message> <key=value...>"
// (component and attributes omitted when empty), so copied logs match
// what the daemon prints to the console.
export function formatLogsForClipboard(logs) {
  if (!Array.isArray(logs) || logs.length === 0) return '';
  return logs
    .map((l) => {
      const comp = l.component ? ` [${l.component}]` : '';
      const attrs = formatAttrs(l.attrs);
      return `${l.timestamp} ${l.level}${comp} ${l.message}${attrs ? ` ${attrs}` : ''}`;
    })
    .join('\n');
}

// shouldAutoscroll returns true when the viewport is at (or within
// `threshold` px of) the bottom, meaning new lines should keep scrolling
// into view. When the user has scrolled up, it returns false so reading
// history is not interrupted.
export function shouldAutoscroll(scrollTop, scrollHeight, clientHeight, threshold = 24) {
  return scrollHeight - (scrollTop + clientHeight) <= threshold;
}

// levelClass maps a slog level string to a css-class suffix used for
// per-line coloring. Unknown levels fall back to "info".
export function levelClass(level) {
  switch (String(level).toLowerCase()) {
    case 'error':
      return 'error';
    case 'warn':
      return 'warn';
    case 'debug':
      return 'debug';
    case 'info':
      return 'info';
    default:
      return 'info';
  }
}
