// Tests for the pure load/cap logic behind the persisted "known
// stations" map used by stationNewTransport.js.
//
// Run with:
//   node --test src/lib/known-stations-core.test.js

import { strict as assert } from 'node:assert';
import { describe, it } from 'node:test';

import { parseKnownStations, serializeKnownStations, MAX_KNOWN_STATIONS } from './known-stations-core.js';

describe('parseKnownStations', () => {
  it('returns an empty Map for null/missing input', () => {
    assert.deepEqual(parseKnownStations(null), new Map());
  });

  it('returns an empty Map for corrupt JSON', () => {
    assert.deepEqual(parseKnownStations('not json'), new Map());
  });

  it('returns an empty Map when the JSON is not an array', () => {
    assert.deepEqual(parseKnownStations('{"a":1}'), new Map());
  });

  it('parses a valid [callsign, ms] pair array', () => {
    const map = parseKnownStations('[["W1ABC-9", 1000], ["W2DEF-1", 2000]]');
    assert.deepEqual(map, new Map([['W1ABC-9', 1000], ['W2DEF-1', 2000]]));
  });

  it('drops malformed entries (wrong shape, wrong types) but keeps valid ones', () => {
    const map = parseKnownStations('[["W1ABC-9", 1000], ["bad"], [42, 1000], ["W2DEF-1", "nope"], ["W3GHI-2", 3000]]');
    assert.deepEqual(map, new Map([['W1ABC-9', 1000], ['W3GHI-2', 3000]]));
  });
});

describe('serializeKnownStations', () => {
  it('round-trips a small map unchanged', () => {
    const map = new Map([['W1ABC-9', 1000], ['W2DEF-1', 2000]]);
    assert.deepEqual(serializeKnownStations(map), [['W1ABC-9', 1000], ['W2DEF-1', 2000]]);
  });

  it('evicts the least-recently-heard entries once over MAX_KNOWN_STATIONS', () => {
    const map = new Map();
    for (let i = 0; i < MAX_KNOWN_STATIONS + 5; i++) map.set(`W${i}`, i); // ms == insertion index
    const out = serializeKnownStations(map);
    assert.equal(out.length, MAX_KNOWN_STATIONS);
    const keys = new Set(out.map(([callsign]) => callsign));
    // The 5 lowest timestamps (oldest hearings) were evicted.
    assert.ok(!keys.has('W0'));
    assert.ok(!keys.has('W4'));
    assert.ok(keys.has('W5'));
    assert.ok(keys.has(`W${MAX_KNOWN_STATIONS + 4}`));
  });

  it('evicts by recency, not insertion order', () => {
    const map = new Map();
    // Insert out of chronological order: the middle one is actually oldest.
    map.set('recent', 3000);
    map.set('oldest', 1000);
    map.set('newest', 4000);
    for (let i = 0; i < MAX_KNOWN_STATIONS; i++) map.set(`W${i}`, 2000 + i);
    const out = serializeKnownStations(map);
    const keys = new Set(out.map(([callsign]) => callsign));
    assert.ok(!keys.has('oldest'));
    assert.ok(keys.has('newest'));
  });
});
