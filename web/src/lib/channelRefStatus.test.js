import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  channelRefStatus,
  buildChannelsById,
  STATUS_OK,
  STATUS_UNREACHABLE,
  STATUS_DELETED,
  REASON_DELETED,
} from './channelRefStatus.js';
import {
  txPredicate,
  isTxCapable,
  TX_REASON_NO_INPUT_DEVICE,
  TX_REASON_NO_OUTPUT_DEVICE,
} from './channelBacking.js';

// ---------------------------------------------------------------------------
// Wire constants. Asserting the literal string values catches accidental
// renames that would desync with the Go backend's TxReason* constants and
// with the JSON status tokens consumed by renderers.
// ---------------------------------------------------------------------------

test('status constants are the expected wire strings', () => {
  assert.equal(STATUS_OK, 'ok');
  assert.equal(STATUS_UNREACHABLE, 'unreachable');
  assert.equal(STATUS_DELETED, 'deleted');
  assert.equal(REASON_DELETED, 'channel deleted');
});

test('tx reason constants mirror the Go dto.TxReason* wire strings', () => {
  assert.equal(TX_REASON_NO_INPUT_DEVICE, 'no input device configured');
  assert.equal(TX_REASON_NO_OUTPUT_DEVICE, 'no output device configured');
});

// ---------------------------------------------------------------------------
// Fixtures. Small builders so intent is readable at the call site; no
// shared mutable state between tests.
// ---------------------------------------------------------------------------

function capableChannel(id, name = `ch${id}`) {
  return { id, name, backing: { tx: { capable: true, reason: '' } } };
}

function incapableChannel(id, reason, name = `ch${id}`) {
  return { id, name, backing: { tx: { capable: false, reason } } };
}

// ---------------------------------------------------------------------------
// channelRefStatus -- truth table
// ---------------------------------------------------------------------------

test('channelRefStatus: returns ok for a TX-capable channel', () => {
  const ch = capableChannel(1, 'VHF');
  const map = new Map([[1, ch]]);
  const result = channelRefStatus(1, map);
  assert.equal(result.status, STATUS_OK);
  assert.equal(result.reason, '');
  assert.equal(result.channel, ch);
});

test('channelRefStatus: unreachable passes through "no input device configured"', () => {
  const ch = incapableChannel(2, TX_REASON_NO_INPUT_DEVICE);
  const map = new Map([[2, ch]]);
  const result = channelRefStatus(2, map);
  assert.equal(result.status, STATUS_UNREACHABLE);
  assert.equal(result.reason, TX_REASON_NO_INPUT_DEVICE);
  assert.equal(result.channel, ch);
});

test('channelRefStatus: unreachable passes through "no output device configured"', () => {
  const ch = incapableChannel(4, TX_REASON_NO_OUTPUT_DEVICE);
  const map = new Map([[4, ch]]);
  const result = channelRefStatus(4, map);
  assert.equal(result.status, STATUS_UNREACHABLE);
  assert.equal(result.reason, TX_REASON_NO_OUTPUT_DEVICE);
  assert.equal(result.channel, ch);
});

test('channelRefStatus: unreachable with empty reason falls back to "not TX-capable"', () => {
  // Defensive: if the server ever returns capable=false with no reason
  // string (DTO contract violation), the helper must still surface a
  // non-empty reason so the pill isn't blank.
  const ch = incapableChannel(5, '');
  const map = new Map([[5, ch]]);
  const result = channelRefStatus(5, map);
  assert.equal(result.status, STATUS_UNREACHABLE);
  assert.equal(result.reason, 'not TX-capable');
  assert.equal(result.channel, ch);
});

test('channelRefStatus: returns deleted when channel is missing from the map', () => {
  const map = new Map([[1, capableChannel(1)]]);
  const result = channelRefStatus(37, map);
  assert.equal(result.status, STATUS_DELETED);
  assert.equal(result.reason, REASON_DELETED);
  assert.equal(result.channel, null);
});

test('channelRefStatus: KISS-only capable channel reports ok (helper is path-agnostic)', () => {
  // The helper reads backing.tx.capable only; it doesn't care whether
  // the underlying backing is a modem or a KISS entry. Phase 1 already
  // decides capability upstream, so this test guards that Phase 3 does
  // not start second-guessing it.
  const ch = capableChannel(3, 'KISS');
  const map = new Map([[3, ch]]);
  const result = channelRefStatus(3, map);
  assert.equal(result.status, STATUS_OK);
  assert.equal(result.reason, '');
  assert.equal(result.channel, ch);
});

// ---------------------------------------------------------------------------
// channelRefStatus -- coercion and edge cases
// ---------------------------------------------------------------------------

test('channelRefStatus: coerces numeric-string channelId to number for lookup', () => {
  const ch = capableChannel(3);
  const map = new Map([[3, ch]]);
  const result = channelRefStatus('3', map);
  assert.equal(result.status, STATUS_OK);
  assert.equal(result.channel, ch);
});

test('channelRefStatus: null channelId returns deleted', () => {
  const map = new Map([[1, capableChannel(1)]]);
  const result = channelRefStatus(null, map);
  assert.equal(result.status, STATUS_DELETED);
  assert.equal(result.reason, REASON_DELETED);
  assert.equal(result.channel, null);
});

test('channelRefStatus: undefined channelId returns deleted', () => {
  const map = new Map([[1, capableChannel(1)]]);
  const result = channelRefStatus(undefined, map);
  assert.equal(result.status, STATUS_DELETED);
  assert.equal(result.channel, null);
});

test('channelRefStatus: zero channelId (soft-FK sentinel) returns deleted', () => {
  const map = new Map([[1, capableChannel(1)]]);
  const result = channelRefStatus(0, map);
  assert.equal(result.status, STATUS_DELETED);
  assert.equal(result.channel, null);
});

test('channelRefStatus: negative channelId returns deleted', () => {
  const map = new Map([[1, capableChannel(1)]]);
  const result = channelRefStatus(-5, map);
  assert.equal(result.status, STATUS_DELETED);
  assert.equal(result.channel, null);
});

test('channelRefStatus: non-numeric-string channelId returns deleted', () => {
  const map = new Map([[1, capableChannel(1)]]);
  const result = channelRefStatus('abc', map);
  assert.equal(result.status, STATUS_DELETED);
  assert.equal(result.channel, null);
});

test('channelRefStatus: non-Map channelsById (plain object) treated as empty', () => {
  // The helper does a `channelsById instanceof Map` guard. Passing a
  // plain object therefore always yields 'deleted', documenting the
  // contract that callers must pass a Map. If this ever changes,
  // update this test rather than silently accepting both shapes.
  const result = channelRefStatus(1, { 1: capableChannel(1) });
  assert.equal(result.status, STATUS_DELETED);
  assert.equal(result.channel, null);
});

test('channelRefStatus: null channelsById treated as empty', () => {
  const result = channelRefStatus(1, null);
  assert.equal(result.status, STATUS_DELETED);
  assert.equal(result.channel, null);
});

test('channelRefStatus: channel missing backing.tx entirely reports unreachable', () => {
  // Degenerate DTO (no backing.tx at all) is semantically the same as
  // !capable. The fallback reason kicks in.
  const ch = { id: 7, name: 'legacy', backing: {} };
  const map = new Map([[7, ch]]);
  const result = channelRefStatus(7, map);
  assert.equal(result.status, STATUS_UNREACHABLE);
  assert.equal(result.reason, 'not TX-capable');
  assert.equal(result.channel, ch);
});

test('channelRefStatus: channel missing backing entirely reports unreachable', () => {
  const ch = { id: 8, name: 'barebones' };
  const map = new Map([[8, ch]]);
  const result = channelRefStatus(8, map);
  assert.equal(result.status, STATUS_UNREACHABLE);
  assert.equal(result.reason, 'not TX-capable');
  assert.equal(result.channel, ch);
});

// ---------------------------------------------------------------------------
// buildChannelsById
// ---------------------------------------------------------------------------

test('buildChannelsById: empty array returns empty Map', () => {
  const m = buildChannelsById([]);
  assert.ok(m instanceof Map);
  assert.equal(m.size, 0);
});

test('buildChannelsById: null returns empty Map', () => {
  const m = buildChannelsById(null);
  assert.ok(m instanceof Map);
  assert.equal(m.size, 0);
});

test('buildChannelsById: undefined returns empty Map', () => {
  const m = buildChannelsById(undefined);
  assert.ok(m instanceof Map);
  assert.equal(m.size, 0);
});

test('buildChannelsById: non-array input returns empty Map', () => {
  assert.equal(buildChannelsById('oops').size, 0);
  assert.equal(buildChannelsById(42).size, 0);
  assert.equal(buildChannelsById({ 0: capableChannel(1) }).size, 0);
});

test('buildChannelsById: keys by numeric id and preserves the channel object', () => {
  const a = capableChannel(1, 'VHF');
  const b = incapableChannel(2, TX_REASON_NO_INPUT_DEVICE, 'UHF');
  const m = buildChannelsById([a, b]);
  assert.equal(m.size, 2);
  assert.equal(m.get(1), a);
  assert.equal(m.get(2), b);
});

test('buildChannelsById: skips entries with non-numeric ids', () => {
  // Guards against backend regressions where id ever comes across as a
  // string. The current DTO guarantees number; we fail closed here.
  const m = buildChannelsById([
    { id: 1, name: 'good' },
    { id: '2', name: 'stringy' },
    { id: null, name: 'null-id' },
    { name: 'no-id' },
    null,
  ]);
  assert.equal(m.size, 1);
  assert.ok(m.has(1));
  assert.ok(!m.has(2));
  assert.ok(!m.has('2'));
});

test('buildChannelsById: on duplicate ids, last write wins', () => {
  // Shouldn't happen in practice (channel ids are unique primary keys),
  // but the helper should not crash. Map semantics => last insertion wins.
  const first = capableChannel(1, 'first');
  const second = capableChannel(1, 'second');
  const m = buildChannelsById([first, second]);
  assert.equal(m.size, 1);
  assert.equal(m.get(1), second);
});

test('buildChannelsById + channelRefStatus end-to-end round trip', () => {
  // Integration-style but still pure: build the map the way a caller
  // would and then classify one of each status.
  const good = capableChannel(1, 'VHF');
  const broken = incapableChannel(2, TX_REASON_NO_OUTPUT_DEVICE, 'UHF');
  const m = buildChannelsById([good, broken]);

  assert.equal(channelRefStatus(1, m).status, STATUS_OK);
  assert.equal(channelRefStatus(2, m).status, STATUS_UNREACHABLE);
  assert.equal(channelRefStatus(2, m).reason, TX_REASON_NO_OUTPUT_DEVICE);
  assert.equal(channelRefStatus(99, m).status, STATUS_DELETED);
});

// ---------------------------------------------------------------------------
// txPredicate
// ---------------------------------------------------------------------------

test('txPredicate: capable channel returns { ok: true, reason: "" }', () => {
  assert.deepEqual(txPredicate(capableChannel(1)), { ok: true, reason: '' });
});

test('txPredicate: incapable channel returns { ok: false, reason } verbatim', () => {
  assert.deepEqual(txPredicate(incapableChannel(2, TX_REASON_NO_INPUT_DEVICE)), {
    ok: false,
    reason: TX_REASON_NO_INPUT_DEVICE,
  });
  assert.deepEqual(txPredicate(incapableChannel(3, TX_REASON_NO_OUTPUT_DEVICE)), {
    ok: false,
    reason: TX_REASON_NO_OUTPUT_DEVICE,
  });
});

test('txPredicate: channel missing backing.tx returns { ok: false, reason: "" }', () => {
  assert.deepEqual(txPredicate({ id: 1, name: 'bare', backing: {} }), {
    ok: false,
    reason: '',
  });
});

test('txPredicate: channel missing backing entirely returns { ok: false, reason: "" }', () => {
  assert.deepEqual(txPredicate({ id: 1, name: 'bare' }), { ok: false, reason: '' });
});

test('txPredicate: null/undefined channel returns { ok: false, reason: "" }', () => {
  assert.deepEqual(txPredicate(null), { ok: false, reason: '' });
  assert.deepEqual(txPredicate(undefined), { ok: false, reason: '' });
});

test('txPredicate: coerces non-boolean capable to Boolean (defensive)', () => {
  // If the server ever sends a truthy non-boolean (e.g. "true") the
  // predicate should still hand back a real boolean.
  const ch = { backing: { tx: { capable: 1, reason: '' } } };
  const out = txPredicate(ch);
  assert.equal(out.ok, true);
  assert.equal(typeof out.ok, 'boolean');
});

// ---------------------------------------------------------------------------
// isTxCapable
// ---------------------------------------------------------------------------

test('isTxCapable: true for a capable channel', () => {
  assert.equal(isTxCapable(capableChannel(1)), true);
});

test('isTxCapable: false for an incapable channel', () => {
  assert.equal(isTxCapable(incapableChannel(2, TX_REASON_NO_INPUT_DEVICE)), false);
});

test('isTxCapable: false when backing.tx is missing', () => {
  assert.equal(isTxCapable({ id: 1, backing: {} }), false);
});

test('isTxCapable: false when backing is missing', () => {
  assert.equal(isTxCapable({ id: 1 }), false);
});

test('isTxCapable: false for null/undefined channel', () => {
  assert.equal(isTxCapable(null), false);
  assert.equal(isTxCapable(undefined), false);
});

test('isTxCapable: always returns a real boolean (not truthy/falsy)', () => {
  assert.equal(typeof isTxCapable(capableChannel(1)), 'boolean');
  assert.equal(typeof isTxCapable(null), 'boolean');
});
