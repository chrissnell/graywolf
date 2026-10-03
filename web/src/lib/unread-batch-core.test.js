import { test } from 'node:test';
import assert from 'node:assert/strict';
import { countByThread, adjustUnread } from './unread-batch-core.js';

const tid = (kind, key) => `${kind}:${key}`;

test('countByThread groups a batch by thread', () => {
  const got = countByThread(
    [
      [1, { kind: 'dm', key: 'N0CALL' }],
      [2, { kind: 'dm', key: 'N0CALL' }],
      [3, { kind: 'tac', key: 'NET' }],
    ],
    tid,
  );
  assert.deepEqual([...got], [['dm:N0CALL', 2], ['tac:NET', 1]]);
});

test('countByThread uses each entry\'s own kind/key, not a shared thread', () => {
  // The chat component persists across thread switches, so a batch can
  // straddle two threads; each message must be charged to its own.
  const got = countByThread(
    [[10, { kind: 'dm', key: 'A' }], [11, { kind: 'dm', key: 'B' }]],
    tid,
  );
  assert.equal(got.get('dm:A'), 1);
  assert.equal(got.get('dm:B'), 1);
});

test('countByThread skips entries with no thread info and handles empty input', () => {
  assert.equal(countByThread([], tid).size, 0);
  assert.equal(countByThread([[1, undefined]], tid).size, 0);
});

test('adjustUnread decrements and rolls back', () => {
  assert.equal(adjustUnread(5, -2), 3);
  assert.equal(adjustUnread(3, 2), 5);
});

test('adjustUnread never goes below zero', () => {
  assert.equal(adjustUnread(1, -3), 0);
  assert.equal(adjustUnread(undefined, -1), 0);
  assert.equal(adjustUnread(undefined, 1), 1);
});
