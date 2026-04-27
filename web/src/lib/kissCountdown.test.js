import test from 'node:test';
import assert from 'node:assert/strict';
import {
  countdownText,
  stateLabel,
  stateBadgeVariant,
  canRetryNow,
  formatLocalTime,
} from './kissCountdown.js';

test('countdownText returns empty when no retry scheduled', () => {
  assert.equal(countdownText(0, 1000), '');
  assert.equal(countdownText(null, 1000), '');
  assert.equal(countdownText(undefined, 1000), '');
});

test('countdownText precise seconds within 30s', () => {
  assert.equal(countdownText(1000 + 5000, 1000), 'Reconnecting in 5s');
  assert.equal(countdownText(1000 + 15500, 1000), 'Reconnecting in 16s');
  assert.equal(countdownText(1000 + 30000, 1000), 'Reconnecting in 30s');
});

test('countdownText coarse minutes beyond 30s', () => {
  // 31s remaining → round up to 1 minute.
  assert.equal(countdownText(1000 + 31000, 1000), 'Reconnecting in ~1m');
  // 90s → 2 minutes (ceil).
  assert.equal(countdownText(1000 + 90000, 1000), 'Reconnecting in ~2m');
  // 3m 0.5s → 4 minutes (still ceil).
  assert.equal(countdownText(1000 + 3 * 60000 + 500, 1000), 'Reconnecting in ~4m');
});

test('countdownText handles fencepost at zero/negative remaining', () => {
  assert.equal(countdownText(1000, 1000), 'Reconnecting now…');
  assert.equal(countdownText(500, 1000), 'Reconnecting now…');
});

test('stateLabel renders friendly text for each state', () => {
  assert.equal(stateLabel('connected'), 'Connected');
  assert.equal(stateLabel('connecting'), 'Connecting…');
  assert.equal(stateLabel('backoff'), 'Reconnecting');
  assert.equal(stateLabel('disconnected'), 'Disconnected');
  assert.equal(stateLabel('listening'), 'Listening');
  assert.equal(stateLabel('stopped'), 'Stopped');
  assert.equal(stateLabel('weird-thing'), 'weird-thing');
  assert.equal(stateLabel(''), 'Unknown');
  assert.equal(stateLabel(undefined), 'Unknown');
});

test('stateBadgeVariant maps to chonky-ui variants', () => {
  assert.equal(stateBadgeVariant('connected'), 'success');
  assert.equal(stateBadgeVariant('listening'), 'success');
  assert.equal(stateBadgeVariant('connecting'), 'info');
  assert.equal(stateBadgeVariant('backoff'), 'warning');
  assert.equal(stateBadgeVariant('disconnected'), 'warning');
  assert.equal(stateBadgeVariant('stopped'), 'error');
  assert.equal(stateBadgeVariant('unknown-x'), 'info');
});

test('canRetryNow returns true only for backoff / disconnected', () => {
  assert.equal(canRetryNow('backoff'), true);
  assert.equal(canRetryNow('disconnected'), true);
  assert.equal(canRetryNow('connected'), false);
  assert.equal(canRetryNow('connecting'), false);
  assert.equal(canRetryNow('listening'), false);
  assert.equal(canRetryNow('stopped'), false);
});

test('formatLocalTime returns empty string for zero', () => {
  assert.equal(formatLocalTime(0), '');
  assert.equal(formatLocalTime(null), '');
});

test('formatLocalTime renders a non-empty time string for a valid timestamp', () => {
  const t = new Date('2026-04-20T12:00:00Z').getTime();
  const s = formatLocalTime(t);
  assert.ok(s.length > 0);
  assert.match(s, /:/);
});
