// Tests for the pure base-callsign matching behind favorite-station
// wildcard matching.
//
// Run with:
//   node --test src/lib/callsign-match-core.test.js

import { strict as assert } from 'node:assert';
import { describe, it } from 'node:test';

import { baseCallsign, callsignMatches } from './callsign-match-core.js';

describe('baseCallsign', () => {
  it('strips a trailing -SSID', () => {
    assert.equal(baseCallsign('KV4S-9'), 'KV4S');
  });

  it('returns the callsign unchanged when there is no SSID', () => {
    assert.equal(baseCallsign('KV4S'), 'KV4S');
  });
});

describe('callsignMatches', () => {
  it('a bare favorite matches the exact bare callsign', () => {
    assert.equal(callsignMatches('KV4S', 'KV4S'), true);
  });

  it('a bare favorite matches any SSID of that base call', () => {
    assert.equal(callsignMatches('KV4S', 'KV4S-9'), true);
    assert.equal(callsignMatches('KV4S', 'KV4S-1'), true);
  });

  it('a bare favorite does NOT match an unrelated callsign that merely shares a prefix', () => {
    // The operator's own example: favoriting "KV4S" must not alert for "KV4SM".
    assert.equal(callsignMatches('KV4S', 'KV4SM'), false);
    assert.equal(callsignMatches('KV4S', 'KV4SM-9'), false);
  });

  it('an SSID-qualified favorite matches only that exact station', () => {
    assert.equal(callsignMatches('KV4S-9', 'KV4S-9'), true);
    assert.equal(callsignMatches('KV4S-9', 'KV4S-1'), false);
    assert.equal(callsignMatches('KV4S-9', 'KV4S'), false);
  });

  it('is case-insensitive', () => {
    assert.equal(callsignMatches('kv4s', 'KV4S-9'), true);
    assert.equal(callsignMatches('KV4S', 'kv4s-9'), true);
  });

  it('handles empty/missing input', () => {
    assert.equal(callsignMatches('', 'KV4S'), false);
    assert.equal(callsignMatches('KV4S', ''), false);
    assert.equal(callsignMatches(null, 'KV4S'), false);
  });
});
