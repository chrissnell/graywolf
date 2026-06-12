import test from 'node:test';
import assert from 'node:assert/strict';
import {
  SIGNIFICANT_MS,
  formatOffsetMagnitude,
  isSignificantOffset,
  offsetFromHeaders,
} from './clock-offset-core.js';

// Minimal case-insensitive stand-in for the Fetch Headers object.
function headers(map) {
  const lower = {};
  for (const [k, v] of Object.entries(map)) lower[k.toLowerCase()] = v;
  return {
    get(name) {
      const v = lower[name.toLowerCase()];
      return v === undefined ? null : v;
    },
  };
}

// A fixed browser "now" keeps the offset math deterministic.
const BROWSER_NOW = Date.parse('2026-06-12T12:00:00Z');
const dateHeaderAt = (ms) => new Date(ms).toUTCString();

test('offsetFromHeaders: valid Date header yields serverNow - browserNow', () => {
  // Host clock 12s ahead of the browser.
  const h = headers({ Date: dateHeaderAt(BROWSER_NOW + 12_000) });
  assert.equal(offsetFromHeaders(h, BROWSER_NOW), 12_000);
});

test('offsetFromHeaders: host behind the browser gives a negative offset', () => {
  const h = headers({ Date: dateHeaderAt(BROWSER_NOW - 30_000) });
  assert.equal(offsetFromHeaders(h, BROWSER_NOW), -30_000);
});

test('offsetFromHeaders: missing Date header is a no-op', () => {
  assert.equal(offsetFromHeaders(headers({}), BROWSER_NOW), null);
});

test('offsetFromHeaders: unparseable Date header is a no-op', () => {
  const h = headers({ Date: 'not a date' });
  assert.equal(offsetFromHeaders(h, BROWSER_NOW), null);
});

test('offsetFromHeaders: null/garbage headers object is a no-op', () => {
  assert.equal(offsetFromHeaders(null, BROWSER_NOW), null);
  assert.equal(offsetFromHeaders(undefined, BROWSER_NOW), null);
  assert.equal(offsetFromHeaders({}, BROWSER_NOW), null);
});

test('offsetFromHeaders: cached response (Age > 0) is ignored', () => {
  // The Date is real but stale — a cache stored it 120s ago. Trusting it would
  // poison the offset, so it must be skipped despite a valid Date.
  const h = headers({ Date: dateHeaderAt(BROWSER_NOW), Age: '120' });
  assert.equal(offsetFromHeaders(h, BROWSER_NOW), null);
});

test('offsetFromHeaders: unparseable Age is treated as cached and ignored', () => {
  const h = headers({ Date: dateHeaderAt(BROWSER_NOW), Age: 'abc' });
  assert.equal(offsetFromHeaders(h, BROWSER_NOW), null);
});

test('offsetFromHeaders: Age: 0 (served fresh) is honored', () => {
  const h = headers({ Date: dateHeaderAt(BROWSER_NOW + 5_000), Age: '0' });
  assert.equal(offsetFromHeaders(h, BROWSER_NOW), 5_000);
});

test('isSignificantOffset: threshold is exactly SIGNIFICANT_MS', () => {
  assert.equal(SIGNIFICANT_MS, 2_000);
  assert.equal(isSignificantOffset(1_999), false);
  assert.equal(isSignificantOffset(2_000), true);
  assert.equal(isSignificantOffset(-2_000), true);
  assert.equal(isSignificantOffset(-1_999), false);
  assert.equal(isSignificantOffset(0), false);
});

test('formatOffsetMagnitude: seconds below 90s', () => {
  assert.equal(formatOffsetMagnitude(0), '0s');
  assert.equal(formatOffsetMagnitude(12_000), '12s');
  assert.equal(formatOffsetMagnitude(-12_000), '12s'); // magnitude only
  assert.equal(formatOffsetMagnitude(89_000), '89s');
});

test('formatOffsetMagnitude: 90s boundary rolls to minutes', () => {
  assert.equal(formatOffsetMagnitude(89_499), '89s'); // rounds to 89s, still <90
  assert.equal(formatOffsetMagnitude(90_000), '2m'); // 90s → 1.5m → round → 2m
});

test('formatOffsetMagnitude: minutes below 90m', () => {
  assert.equal(formatOffsetMagnitude(5 * 60_000), '5m');
  assert.equal(formatOffsetMagnitude(89 * 60_000), '89m');
});

test('formatOffsetMagnitude: 90m boundary rolls to hours', () => {
  assert.equal(formatOffsetMagnitude(90 * 60_000), '2h'); // 90m → 1.5h → round → 2h
  assert.equal(formatOffsetMagnitude(3 * 60 * 60_000), '3h');
});
