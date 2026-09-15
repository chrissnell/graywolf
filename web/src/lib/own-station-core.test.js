// Tests for the pure "is this my own station" helper used by
// stationNewTransport.js to suppress new-station/favorite notifications
// for the operator's own station.
//
// Run with:
//   node --test src/lib/own-station-core.test.js

import { strict as assert } from 'node:assert';
import { describe, it } from 'node:test';

import { buildOwnCallsignSet, isOwnStation } from './own-station-core.js';

describe('buildOwnCallsignSet', () => {
  it('includes the base station callsign, uppercased and trimmed', () => {
    const set = buildOwnCallsignSet(' kv4s-1 ', []);
    assert.deepEqual([...set], ['KV4S-1']);
  });

  it('includes non-empty per-beacon callsign overrides', () => {
    const set = buildOwnCallsignSet('KV4S-1', [{ callsign: '' }, { callsign: 'kv4s-9' }]);
    assert.deepEqual([...set].sort(), ['KV4S-1', 'KV4S-9']);
  });

  it('ignores beacons with an empty/missing callsign override (inherit)', () => {
    const set = buildOwnCallsignSet('KV4S-1', [{ callsign: '' }, {}, { callsign: '   ' }]);
    assert.deepEqual([...set], ['KV4S-1']);
  });

  it('handles an empty station callsign and no beacons', () => {
    const set = buildOwnCallsignSet('', []);
    assert.deepEqual([...set], []);
    assert.deepEqual([...buildOwnCallsignSet(null, null)], []);
  });

  it('dedupes when a beacon override matches the base callsign', () => {
    const set = buildOwnCallsignSet('KV4S-1', [{ callsign: 'KV4S-1' }]);
    assert.deepEqual([...set], ['KV4S-1']);
  });
});

describe('isOwnStation', () => {
  it('matches the exact callsign, case-insensitively', () => {
    const set = buildOwnCallsignSet('KV4S-1', []);
    assert.equal(isOwnStation(set, 'kv4s-1'), true);
    assert.equal(isOwnStation(set, 'KV4S-1'), true);
  });

  it('does not match a different SSID of the same base call', () => {
    // KV4S-7 (a separate handheld) must still notify -- exact match only,
    // never a base-callsign wildcard like favorites/exclusions use.
    const set = buildOwnCallsignSet('KV4S-1', []);
    assert.equal(isOwnStation(set, 'KV4S-7'), false);
  });

  it('does not match an unrelated callsign', () => {
    const set = buildOwnCallsignSet('KV4S-1', []);
    assert.equal(isOwnStation(set, 'W1ABC-9'), false);
  });

  it('handles a missing/empty callsign and an empty set', () => {
    const set = buildOwnCallsignSet('KV4S-1', []);
    assert.equal(isOwnStation(set, ''), false);
    assert.equal(isOwnStation(set, null), false);
    assert.equal(isOwnStation(new Set(), 'KV4S-1'), false);
  });
});
