import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  formatLogsForClipboard,
  formatAttrs,
  formatAttrValue,
  shouldAutoscroll,
  levelClass,
} from './systemLogs.js';

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

test('formatLogsForClipboard appends structured attrs', () => {
  const logs = [
    {
      timestamp: '2026-06-07T00:00:00Z',
      level: 'INFO',
      component: 'aprs',
      message: 'aprs packet',
      attrs: { type: 'mic-e', source: 'NW5W-5', comment: 'Static park' },
    },
  ];
  assert.equal(
    formatLogsForClipboard(logs),
    '2026-06-07T00:00:00Z INFO [aprs] aprs packet type=mic-e source=NW5W-5 comment="Static park"',
  );
});

test('formatAttrs renders key=value, quoting values that need it', () => {
  assert.equal(formatAttrs(null), '');
  assert.equal(formatAttrs({}), '');
  assert.equal(formatAttrs({ a: 1, b: true }), 'a=1 b=true');
  assert.equal(formatAttrs({ path: '[K0TFU* WIDE1*]' }), 'path="[K0TFU* WIDE1*]"');
});

test('formatAttrValue quotes empty, spaced, and special values', () => {
  assert.equal(formatAttrValue('plain'), 'plain');
  assert.equal(formatAttrValue(''), '""');
  assert.equal(formatAttrValue('has space'), '"has space"');
  assert.equal(formatAttrValue('k=v'), '"k=v"');
  assert.equal(formatAttrValue(42), '42');
  assert.equal(formatAttrValue(null), '<nil>');
  assert.equal(formatAttrValue({ x: 1 }), '"{\\"x\\":1}"');
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
