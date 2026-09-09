import test from 'node:test';
import assert from 'node:assert/strict';
import { groupBeaconsByChannel } from './beaconsByChannel.js';

test('groupBeaconsByChannel: groups real-channel beacons by their own channel', () => {
  const beacons = [
    { id: 1, channel: 2, enabled: true },
    { id: 2, channel: 3, enabled: true },
    { id: 3, channel: 2, enabled: true },
  ];
  const result = groupBeaconsByChannel(beacons, 2);
  assert.deepEqual(Object.keys(result).sort(), ['2', '3']);
  assert.equal(result[2].length, 2);
  assert.equal(result[3].length, 1);
});

test('groupBeaconsByChannel: buckets Auto (channel=0) beacons under firstTxChannelId', () => {
  const beacons = [
    { id: 1, channel: 0, enabled: true },
    { id: 2, channel: 5, enabled: true },
  ];
  const result = groupBeaconsByChannel(beacons, 5);
  assert.deepEqual(Object.keys(result).sort(), ['5']);
  assert.equal(result[5].length, 2);
});

test('groupBeaconsByChannel: excludes disabled beacons', () => {
  const beacons = [
    { id: 1, channel: 2, enabled: false },
    { id: 2, channel: 2, enabled: true },
  ];
  const result = groupBeaconsByChannel(beacons, 2);
  assert.equal(result[2].length, 1);
  assert.equal(result[2][0].id, 2);
});

test('groupBeaconsByChannel: drops an Auto beacon when there is no TX-capable channel', () => {
  const beacons = [{ id: 1, channel: 0, enabled: true }];
  const result = groupBeaconsByChannel(beacons, null);
  assert.deepEqual(result, {});
});

test('groupBeaconsByChannel: empty beacons array returns an empty object', () => {
  assert.deepEqual(groupBeaconsByChannel([], 1), {});
  assert.deepEqual(groupBeaconsByChannel(undefined, 1), {});
});
