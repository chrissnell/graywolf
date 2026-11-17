// Dashboard-only, browser-local presentation. No packet retention/API changes.
export const DEFAULT_LOG_HEIGHT = 400;
export const MIN_LOG_HEIGHT = 200;
export const MAX_LOG_HEIGHT = 1600;
export const LOG_HEIGHT_KEY = 'dashboard-packet-log-height';

export function normalizeLogHeight(value, max = MAX_LOG_HEIGHT) {
  const n = typeof value === 'number' || (typeof value === 'string' && value.trim())
    ? Number(value) : NaN;
  return Math.round(Math.max(MIN_LOG_HEIGHT, Math.min(max,
    Number.isFinite(n) ? n : DEFAULT_LOG_HEIGHT)));
}

export function maxLogHeight(contentHeight) {
  return Math.min(MAX_LOG_HEIGHT, Math.max(DEFAULT_LOG_HEIGHT,
    Number.isFinite(contentHeight) ? Math.ceil(contentHeight) : DEFAULT_LOG_HEIGHT));
}

export function draggedLogHeight(startHeight, deltaY, scale, max) {
  return normalizeLogHeight(startHeight + deltaY / (scale > 0 ? scale : 1), max);
}

export function keyedLogHeight(key, height, max) {
  const next = {
    ArrowUp: height - 40, ArrowDown: height + 40,
    PageUp: height - 200, PageDown: height + 200,
    Home: MIN_LOG_HEIGHT, End: max,
  }[key];
  return next === undefined ? null : normalizeLogHeight(next, max);
}

export function readLogHeight(storage) {
  try { return normalizeLogHeight((storage ?? globalThis.localStorage).getItem(LOG_HEIGHT_KEY)); }
  catch { return DEFAULT_LOG_HEIGHT; }
}

export function saveLogHeight(height, storage) {
  try { (storage ?? globalThis.localStorage).setItem(LOG_HEIGHT_KEY, String(normalizeLogHeight(height))); }
  catch { /* Storage denial never prevents resizing. */ }
}
