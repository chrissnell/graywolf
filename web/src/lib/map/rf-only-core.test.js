import { test } from 'node:test';
import assert from 'node:assert/strict';
import { isRfOnly } from './rf-only-core.js';

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
