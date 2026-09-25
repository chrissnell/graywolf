// Tests for the pure "newly heard" (threshold-crossing) diffing helper
// used by stationNewTransport.js.
//
// Run with:
//   node --test src/lib/station-new-diff-core.test.js

import { strict as assert } from 'node:assert';
import { describe, it } from 'node:test';

import { diffNewlyHeard } from './station-new-diff-core.js';

const HOUR_MS = 60 * 60 * 1000;
const T0 = Date.parse('2026-07-31T00:00:00Z');

function row(callsign, isoTime) {
  return { callsign, last_heard: isoTime };
}

describe('diffNewlyHeard', () => {
  it('treats a callsign never seen before as newly-crossed, and records it', () => {
    const known = new Map();
    const roster = [row('W1NEW-9', '2026-07-31T00:00:00Z')];
    assert.deepEqual(diffNewlyHeard(known, roster, HOUR_MS * 2), roster);
    assert.equal(known.get('W1NEW-9'), T0);
  });

  it('does not re-fire for a station heard again well within the threshold', () => {
    const known = new Map([['W1ABC-9', T0]]);
    const roster = [row('W1ABC-9', '2026-07-31T00:30:00Z')]; // 30 min later
    assert.deepEqual(diffNewlyHeard(known, roster, HOUR_MS * 2), []);
    // Still records the refreshed timestamp.
    assert.equal(known.get('W1ABC-9'), Date.parse('2026-07-31T00:30:00Z'));
  });

  it('fires again once the gap since last recorded hearing meets the threshold', () => {
    const known = new Map([['W1ABC-9', T0]]);
    const roster = [row('W1ABC-9', '2026-07-31T02:00:00Z')]; // exactly 2h later
    assert.deepEqual(diffNewlyHeard(known, roster, HOUR_MS * 2), roster);
  });

  it('does not fire when the gap is just under the threshold', () => {
    const known = new Map([['W1ABC-9', T0]]);
    const roster = [row('W1ABC-9', '2026-07-31T01:59:59Z')];
    assert.deepEqual(diffNewlyHeard(known, roster, HOUR_MS * 2), []);
  });

  it('Infinity threshold reproduces once-per-lifetime behavior', () => {
    const known = new Map();
    const first = [row('W1ABC-9', '2026-07-31T00:00:00Z')];
    assert.deepEqual(diffNewlyHeard(known, first, Infinity), first);
    const muchLater = [row('W1ABC-9', '2026-08-15T00:00:00Z')];
    assert.deepEqual(diffNewlyHeard(known, muchLater, Infinity), []);
  });

  it('handles multiple rows, mixing crossed and not-yet-crossed', () => {
    const known = new Map([
      ['W1OLD-9', T0], // heard 30 min ago, under threshold
    ]);
    const roster = [
      row('W1OLD-9', '2026-07-31T00:30:00Z'),
      row('W1FRESH-9', '2026-07-31T00:30:00Z'), // never seen before
    ];
    assert.deepEqual(diffNewlyHeard(known, roster, HOUR_MS * 2), [roster[1]]);
  });

  it('ignores malformed rows (no callsign, unparseable last_heard)', () => {
    const known = new Map();
    const roster = [{ last_heard: '2026-07-31T00:00:00Z' }, row('W1BAD-9', 'not-a-date'), row('W1OK-9', '2026-07-31T00:00:00Z')];
    assert.deepEqual(diffNewlyHeard(known, roster, HOUR_MS), [roster[2]]);
    assert.ok(!known.has('W1BAD-9'));
  });

  it('handles an empty or missing roster', () => {
    const known = new Map([['W1OLD-9', T0]]);
    assert.deepEqual(diffNewlyHeard(known, [], HOUR_MS), []);
    assert.deepEqual(diffNewlyHeard(known, undefined, HOUR_MS), []);
  });

  describe('resumeSinceMs (catch-up after the app was closed/suspended)', () => {
    it('fires for a row heard at/after the resume cutoff, even under the per-station threshold', () => {
      // Heard 5 min before "now" -- well under a 4h threshold -- but that
      // 5-minute-old packet arrived entirely within the away window, so
      // the operator never saw it: KV4S-7's reopen-after-a-few-hours report.
      const known = new Map([['KV4S-7', T0]]);
      const resumeSinceMs = T0 + 3 * HOUR_MS; // client resumed 3h after last recorded hearing
      const roster = [row('KV4S-7', new Date(T0 + 3 * HOUR_MS + 5 * 60_000).toISOString())];
      assert.deepEqual(diffNewlyHeard(known, roster, HOUR_MS * 4, resumeSinceMs), roster);
    });

    it('does not fire for a row heard before the resume cutoff (already known before the gap)', () => {
      const known = new Map([['W1ABC-9', T0]]);
      const resumeSinceMs = T0 + 3 * HOUR_MS;
      // Heard 10 min before the client resumed -- i.e. the client should
      // have caught it on a normal poll, this isn't a "missed it" case --
      // and the per-station gap (just under 3h) is under the 4h threshold.
      const roster = [row('W1ABC-9', new Date(resumeSinceMs - 10 * 60_000).toISOString())];
      assert.deepEqual(diffNewlyHeard(known, roster, HOUR_MS * 4, resumeSinceMs), []);
    });

    it('does not repeatedly re-fire a continuously-active station once resumeSinceMs is null again', () => {
      // Simulates the poll immediately after the catch-up one: the
      // transport only passes a non-null resumeSinceMs on the single
      // poll right after detecting a real gap.
      const known = new Map([['KV4S-7', T0 + 3 * HOUR_MS + 5 * 60_000]]);
      const roster = [row('KV4S-7', new Date(T0 + 3 * HOUR_MS + 6 * 60_000).toISOString())]; // 1 min later
      assert.deepEqual(diffNewlyHeard(known, roster, HOUR_MS * 4, null), []);
    });

    it('is a no-op when resumeSinceMs is omitted (default behavior unchanged)', () => {
      const known = new Map([['W1ABC-9', T0]]);
      const roster = [row('W1ABC-9', '2026-07-31T00:30:00Z')];
      assert.deepEqual(diffNewlyHeard(known, roster, HOUR_MS * 2), []);
    });
  });
});
