import { test } from 'node:test';
import assert from 'node:assert/strict';
import { groupReferrers, totalReferrers, REFERRER_TYPE } from './channelReferrers.js';

test('groupReferrers empty list', () => {
  assert.deepEqual(groupReferrers([]), []);
  assert.deepEqual(groupReferrers(null), []);
  assert.deepEqual(groupReferrers(undefined), []);
});

test('groupReferrers preserves group order and counts', () => {
  const groups = groupReferrers([
    { type: REFERRER_TYPE.IGATE_FILTER, id: 3, name: 'callsign: N0CALL' },
    { type: REFERRER_TYPE.BEACON, id: 1, name: 'N0CALL (position)' },
    { type: REFERRER_TYPE.BEACON, id: 2, name: 'N0CALL (object)' },
    { type: REFERRER_TYPE.DIGI_FROM, id: 10, name: 'WIDE (ch 1)' },
    { type: REFERRER_TYPE.IGATE_RF, id: 1, name: '' },
  ]);
  assert.equal(groups[0].type, REFERRER_TYPE.BEACON);
  assert.equal(groups[0].label, 'Beacons'); // plural — 2 items
  assert.equal(groups[0].items.length, 2);
  assert.equal(groups[1].type, REFERRER_TYPE.DIGI_FROM);
  assert.equal(groups[1].label, 'Digipeater rule'); // singular — 1 item
  assert.equal(groups[2].type, REFERRER_TYPE.IGATE_FILTER);
  // iGate RF channel comes near the end per ORDER.
  assert.equal(groups.at(-1).type, REFERRER_TYPE.IGATE_RF);
});

test('groupReferrers surfaces unknown types at the end', () => {
  const groups = groupReferrers([
    { type: 'future_type', id: 99, name: 'x' },
    { type: REFERRER_TYPE.BEACON, id: 1, name: 'b' },
  ]);
  assert.equal(groups[0].type, REFERRER_TYPE.BEACON);
  assert.equal(groups.at(-1).type, 'future_type');
});

test('totalReferrers counts flat list length', () => {
  assert.equal(totalReferrers(null), 0);
  assert.equal(totalReferrers([]), 0);
  assert.equal(totalReferrers([{ type: 'a' }, { type: 'b' }, { type: 'c' }]), 3);
});
