import { test } from 'node:test';
import assert from 'node:assert/strict';
import { computeCotSchedule, formatHMS } from './cotSchedule.js';

test('matches the confirmed example: 4 transmits, 300s delay, decay 2', () => {
  const got = computeCotSchedule(4, 300, 2);
  const wantOffsets = [0, 300, 600, 1200];
  const wantHMS = ['00:00:00', '00:05:00', '00:10:00', '00:20:00'];
  assert.equal(got.length, 4);
  got.forEach((row, i) => {
    assert.equal(row.n, i + 1);
    assert.equal(row.offsetSeconds, wantOffsets[i]);
    assert.equal(row.hms, wantHMS[i]);
  });
});

test('a single transmit has only the immediate offset', () => {
  const got = computeCotSchedule(1, 300, 2);
  assert.deepEqual(got.map((r) => r.offsetSeconds), [0]);
});

test('decay factor 1 gives constant spacing', () => {
  const got = computeCotSchedule(4, 300, 1);
  assert.deepEqual(got.map((r) => r.offsetSeconds), [0, 300, 300, 300]);
});

test('zero or negative transmits returns an empty schedule', () => {
  assert.deepEqual(computeCotSchedule(0, 300, 2), []);
  assert.deepEqual(computeCotSchedule(-1, 300, 2), []);
});

test('formatHMS zero-pads hours/minutes/seconds', () => {
  assert.equal(formatHMS(0), '00:00:00');
  assert.equal(formatHMS(65), '00:01:05');
  assert.equal(formatHMS(3661), '01:01:01');
});
