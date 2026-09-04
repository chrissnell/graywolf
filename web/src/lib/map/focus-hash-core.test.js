// Tests for the pure #/map?focus= deep-link parsing/comparison behind
// LiveMapV2.svelte's focus-popup flow. This logic has caused three
// separate click-through bugs found only by manual testing (invariants
// #66, #67, #68) -- these tests cover the two of those (#66, #68) that
// are pure parsing/comparison logic, not poll-timing races.
//
// Run with:
//   node --test src/lib/map/focus-hash-core.test.js

import { strict as assert } from 'node:assert';
import { describe, it } from 'node:test';

import { parseFocusHash, sameFocus } from './focus-hash-core.js';

describe('parseFocusHash', () => {
  it('parses a well-formed focus hash', () => {
    assert.deepEqual(parseFocusHash('#/map?focus=W1ABC-9&lat=40.5&lon=-105.25'), {
      callsign: 'W1ABC-9',
      lat: 40.5,
      lon: -105.25,
    });
  });

  it('discards the whole link (even the callsign) when lat is missing -- invariant #66', () => {
    assert.equal(parseFocusHash('#/map?focus=W1ABC-9&lon=-105.25'), null);
  });

  it('discards the whole link when lon is missing', () => {
    assert.equal(parseFocusHash('#/map?focus=W1ABC-9&lat=40.5'), null);
  });

  it('discards the link when lat/lon are non-finite (garbage values)', () => {
    assert.equal(parseFocusHash('#/map?focus=W1ABC-9&lat=abc&lon=-105.25'), null);
    assert.equal(parseFocusHash('#/map?focus=W1ABC-9&lat=NaN&lon=-105.25'), null);
  });

  it('returns null for a hash with no querystring at all', () => {
    assert.equal(parseFocusHash('#/map'), null);
    assert.equal(parseFocusHash(''), null);
    assert.equal(parseFocusHash(null), null);
    assert.equal(parseFocusHash(undefined), null);
  });

  it('returns null for an unrelated route hash', () => {
    assert.equal(parseFocusHash('#/messages?thread=dm:W1ABC-9'), null);
  });

  it('tolerates a missing focus param (empty callsign) as long as lat/lon are present', () => {
    assert.deepEqual(parseFocusHash('#/map?lat=40.5&lon=-105.25'), { callsign: '', lat: 40.5, lon: -105.25 });
  });
});

describe('sameFocus', () => {
  it('true for identical callsign/lat/lon', () => {
    const a = { callsign: 'W1ABC-9', lat: 40.5, lon: -105.25 };
    const b = { callsign: 'W1ABC-9', lat: 40.5, lon: -105.25 };
    assert.equal(sameFocus(a, b), true);
  });

  it('false when the callsign differs -- the case that motivated invariant #68', () => {
    const a = { callsign: 'W1ABC-9', lat: 40.5, lon: -105.25 };
    const b = { callsign: 'AK4ZX-15', lat: 34.52, lon: -84.34 };
    assert.equal(sameFocus(a, b), false);
  });

  it('false when only lat/lon differ (same callsign, moved)', () => {
    const a = { callsign: 'W1ABC-9', lat: 40.5, lon: -105.25 };
    const b = { callsign: 'W1ABC-9', lat: 41.0, lon: -106.0 };
    assert.equal(sameFocus(a, b), false);
  });

  it('false when either side is null/undefined', () => {
    const a = { callsign: 'W1ABC-9', lat: 40.5, lon: -105.25 };
    assert.equal(sameFocus(a, null), false);
    assert.equal(sameFocus(null, a), false);
    assert.equal(sameFocus(null, null), false);
  });
});
