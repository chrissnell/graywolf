import { test } from 'node:test';
import assert from 'node:assert/strict';
import { formatLogsForClipboard, shouldAutoscroll, levelClass } from './systemLogs.js';

test('formatLogsForClipboard renders one tab-free line per entry', () => {
  const logs = [
    { timestamp: '2026-06-07T00:00:00Z', level: 'INFO', component: 'webapi', message: 'started' },
    { timestamp: '2026-06-07T00:00:01Z', level: 'WARN', component: '', message: 'careful' },
  ];
  const out = formatLogsForClipboard(logs);
  assert.equal(
    out,
    '2026-06-07T00:00:00Z INFO [webapi] started\n2026-06-07T00:00:01Z WARN careful',
  );
});

test('formatLogsForClipboard handles empty input', () => {
  assert.equal(formatLogsForClipboard([]), '');
  assert.equal(formatLogsForClipboard(null), '');
});

test('shouldAutoscroll true when within threshold of bottom', () => {
  // scrollTop 900 + clientHeight 100 = 1000 == scrollHeight -> at bottom
  assert.equal(shouldAutoscroll(900, 1000, 100, 24), true);
  // 24px from bottom, threshold 24 -> still autoscroll
  assert.equal(shouldAutoscroll(876, 1000, 100, 24), true);
});

test('shouldAutoscroll false when scrolled up past threshold', () => {
  assert.equal(shouldAutoscroll(500, 1000, 100, 24), false);
});

test('levelClass maps slog levels to css suffixes', () => {
  assert.equal(levelClass('ERROR'), 'error');
  assert.equal(levelClass('warn'), 'warn');
  assert.equal(levelClass('INFO'), 'info');
  assert.equal(levelClass('DEBUG'), 'debug');
  assert.equal(levelClass('weird'), 'info');
});
