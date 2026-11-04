// Tests for the pure "newly alerting" diffing helper used by
// stationAlertsTransport.js to decide which stations currently in
// Emergency status should raise a notification on each poll.
//
// Run with:
//   node --test src/lib/station-alerts-diff-core.test.js

import { strict as assert } from 'node:assert';
import { describe, it } from 'node:test';

import { diffNewlyAlerting } from './station-alerts-diff-core.js';

function row(callsign) {
  return { callsign, status_code: 0, status_text: 'Emergency', lat: 40.0, lon: -105.0, last_heard: '2026-07-27T00:00:00Z' };
}

describe('diffNewlyAlerting', () => {
  it('includes a callsign not present in the previous active set', () => {
    const prev = new Set();
    const fresh = [row('W1EMG-9')];
    assert.deepEqual(diffNewlyAlerting(prev, fresh), fresh);
  });

  it('excludes a callsign still alerting from the previous poll', () => {
    const prev = new Set(['W1EMG-9']);
    const fresh = [row('W1EMG-9')];
    assert.deepEqual(diffNewlyAlerting(prev, fresh), []);
  });

  it('re-includes a callsign that cleared and re-asserted Emergency', () => {
    // stationAlertsTransport.js replaces its active set with the fresh
    // poll's callsigns every cycle, so a station absent from one poll
    // (status changed away from Emergency) and present again later is
    // "not in prevActive" on that later poll -- i.e. newly alerting.
    const prev = new Set(); // W1EMG-9 was NOT in the immediately preceding poll
    const fresh = [row('W1EMG-9')];
    assert.deepEqual(diffNewlyAlerting(prev, fresh), fresh);
  });

  it('handles multiple rows, mixing new and already-active', () => {
    const prev = new Set(['W1OLD-9']);
    const fresh = [row('W1OLD-9'), row('W1NEW-9')];
    assert.deepEqual(diffNewlyAlerting(prev, fresh), [fresh[1]]);
  });

  it('ignores malformed rows without a callsign', () => {
    const prev = new Set();
    const fresh = [{ status_code: 0 }, row('W1EMG-9')];
    assert.deepEqual(diffNewlyAlerting(prev, fresh), [fresh[1]]);
  });

  it('handles an empty fresh snapshot', () => {
    const prev = new Set(['W1EMG-9']);
    assert.deepEqual(diffNewlyAlerting(prev, []), []);
    assert.deepEqual(diffNewlyAlerting(prev, undefined), []);
  });
});
