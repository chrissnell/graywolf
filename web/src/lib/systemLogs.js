// Pure helpers for the System Logs tab. Kept framework-free so they can
// be unit-tested with `node --test` (the repo's frontend test runner).

// formatLogsForClipboard renders log entries as plain text, one line per
// entry: "<timestamp> <LEVEL> [component] <message>" (component omitted
// when empty).
export function formatLogsForClipboard(logs) {
  if (!Array.isArray(logs) || logs.length === 0) return '';
  return logs
    .map((l) => {
      const comp = l.component ? ` [${l.component}]` : '';
      return `${l.timestamp} ${l.level}${comp} ${l.message}`;
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
