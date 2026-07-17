import { test } from 'node:test';
import assert from 'node:assert/strict';
import { isRfOnly, rfReachableDespiteNonRfLatest } from './rf-only-core.js';

test('station whose current fix was heard directly on RF qualifies', () => {
  const station = { positions: [{ direction: 'RX', hops: 0 }] };
  assert.equal(isRfOnly(station), true);
});

test('station whose current fix was RF-digipeated qualifies', () => {
  const station = { positions: [{ direction: 'RX', hops: 2 }] };
  assert.equal(isRfOnly(station), true);
});

test('station whose current fix arrived via APRS-IS is excluded', () => {
  const station = { positions: [{ direction: 'IS' }] };
  assert.equal(isRfOnly(station), false);
});

test('station whose current fix was Internet-to-RF gated is excluded', () => {
  const station = { positions: [{ direction: 'RX', gated: true }] };
  assert.equal(isRfOnly(station), false);
});

test('gated wins over the RF-digipeated allowance', () => {
  // A gated copy that also carries digi hops must still be dropped: gated is
  // disqualifying regardless of hops, unlike a plain RF-digipeated fix.
  const station = { positions: [{ direction: 'RX', hops: 2, gated: true }] };
  assert.equal(isRfOnly(station), false);
});

test('our own transmission (TX) is excluded', () => {
  // The filter qualifies on RX only, not merely "not IS"; our own TX beacon
  // is not an RF reception of another station.
  const station = { positions: [{ direction: 'TX', hops: 0 }] };
  assert.equal(isRfOnly(station), false);
});

test('current APRS-IS fix wins over a stale RF breadcrumb in the trail (#394)', () => {
  // Moving station heard on RF earlier, now arriving only via APRS-IS. The
  // marker/popup show the APRS-IS fix, so RF Only must hide it even though an
  // older RF fix lingers in the accumulated trail.
  const station = {
    positions: [
      { direction: 'IS' },
      { direction: 'RX', hops: 0 },
    ],
  };
  assert.equal(isRfOnly(station), false);
});

test('missing or empty positions are excluded', () => {
  assert.equal(isRfOnly({ positions: [] }), false);
  assert.equal(isRfOnly({}), false);
  assert.equal(isRfOnly(null), false);
});

// rfReachableDespiteNonRfLatest — the popup "RF-reachable" note case (#482):
// plotted fix is RF-heard but the latest packet arrived some other way.

test('RF-reachable note fires when plotted fix is RF but latest packet is APRS-IS', () => {
  // The K4MMG-10 shape from #482: rfRank-protected RX head, latest packet IS.
  const station = {
    direction: 'IS',
    gated: false,
    positions: [{ direction: 'RX', hops: 0 }],
  };
  assert.equal(rfReachableDespiteNonRfLatest(station), true);
});

test('RF-reachable note fires for a digipeated RF fix with an APRS-IS latest packet', () => {
  const station = {
    direction: 'IS',
    positions: [{ direction: 'RX', hops: 2 }],
  };
  assert.equal(rfReachableDespiteNonRfLatest(station), true);
});

test('RF-reachable note fires when the latest packet is Internet-to-RF gated', () => {
  // Latest arrival gated (not RF), plotted fix still RF-heard.
  const station = {
    direction: 'RX',
    gated: true,
    positions: [{ direction: 'RX', hops: 0 }],
  };
  assert.equal(rfReachableDespiteNonRfLatest(station), true);
});

test('no RF-reachable note when the latest packet itself arrived over RF', () => {
  // Badge/via already say RF — nothing to disambiguate.
  const station = {
    direction: 'RX',
    gated: false,
    positions: [{ direction: 'RX', hops: 0 }],
  };
  assert.equal(rfReachableDespiteNonRfLatest(station), false);
});

test('no RF-reachable note for a pure APRS-IS station (plotted fix is IS)', () => {
  const station = {
    direction: 'IS',
    positions: [{ direction: 'IS' }],
  };
  assert.equal(rfReachableDespiteNonRfLatest(station), false);
});

test('no RF-reachable note when the plotted fix is Internet-to-RF gated', () => {
  const station = {
    direction: 'IS',
    positions: [{ direction: 'RX', gated: true }],
  };
  assert.equal(rfReachableDespiteNonRfLatest(station), false);
});

test('RF-reachable note fires when the latest packet is our own TX but the fix was RF-heard', () => {
  // Our own beacon digipeated back and heard on RF (RX plotted fix), then a
  // later TX beacon flips the station-level direction to TX. TX is not RF
  // reception, so the note fires — the tooltip wording is deliberately generic
  // ("did not arrive over RF") rather than APRS-IS-specific to stay truthful here.
  const station = {
    direction: 'TX',
    positions: [{ direction: 'RX', hops: 1 }],
  };
  assert.equal(rfReachableDespiteNonRfLatest(station), true);
});

test('no RF-reachable note for missing/empty positions or undefined station', () => {
  assert.equal(rfReachableDespiteNonRfLatest({ direction: 'IS', positions: [] }), false);
  assert.equal(rfReachableDespiteNonRfLatest({ direction: 'IS' }), false);
  assert.equal(rfReachableDespiteNonRfLatest({}), false);
  assert.equal(rfReachableDespiteNonRfLatest(null), false);
});
