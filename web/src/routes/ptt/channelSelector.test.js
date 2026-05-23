// web/src/routes/ptt/channelSelector.test.js
import { test } from 'node:test';
import assert from 'node:assert/strict';

import {
  modemBackedChannels,
  channelsNeedingPtt,
  showChannelSelector,
  showAddButton,
  pttDetectionBlockedReason,
} from './channelSelector.js';

test('modemBackedChannels filters out channels with null input_device_id', () => {
  const channels = [
    { id: 1, name: 'a', input_device_id: 10, mode: 'aprs' },
    { id: 2, name: 'b', input_device_id: null, mode: 'aprs' },
    { id: 3, name: 'c', input_device_id: 20, mode: 'packet' },
  ];
  // packet-mode is allowed in modemBackedChannels; the PTT-needs rule
  // only filters on input_device_id (matching the spec).
  assert.deepEqual(modemBackedChannels(channels).map(c => c.id), [1, 3]);
});

test('channelsNeedingPtt returns modem-backed channels without a PttConfig row', () => {
  const channels = [
    { id: 1, name: 'a', input_device_id: 10 },
    { id: 2, name: 'b', input_device_id: 20 },
    { id: 3, name: 'c', input_device_id: null },
  ];
  const pttByChannel = new Map([[1, { method: 'serial_rts' }]]);
  assert.deepEqual(channelsNeedingPtt(channels, pttByChannel).map(c => c.id), [2]);
});

test('showChannelSelector is true only when >1 channel needs PTT', () => {
  const c1 = { id: 1, input_device_id: 10 };
  const c2 = { id: 2, input_device_id: 20 };
  assert.equal(showChannelSelector([c1, c2], new Map()), true);
  assert.equal(showChannelSelector([c1], new Map()), false);
  assert.equal(showChannelSelector([], new Map()), false);
});

test('showAddButton is true when at least one channel needs PTT', () => {
  const c1 = { id: 1, input_device_id: 10 };
  assert.equal(showAddButton([c1], new Map()), true);
  assert.equal(showAddButton([c1], new Map([[1, {}]])), false);
  assert.equal(showAddButton([], new Map()), false);
});

test('pttDetectionBlockedReason is null when a channel can accept a PTT config', () => {
  const c1 = { id: 1, input_device_id: 10 };
  assert.equal(pttDetectionBlockedReason([c1], new Map()), null);
});

test('pttDetectionBlockedReason flags no-modem-channel when no audio-modem channel exists', () => {
  // Zero channels at all.
  assert.equal(pttDetectionBlockedReason([], new Map()), 'no-modem-channel');
  // Only KISS-TNC channels (input_device_id == null) — none are PTT-eligible.
  const kiss = [{ id: 1, input_device_id: null }];
  assert.equal(pttDetectionBlockedReason(kiss, new Map()), 'no-modem-channel');
});

test('pttDetectionBlockedReason flags all-configured when every modem channel already has PTT', () => {
  const c1 = { id: 1, input_device_id: 10 };
  const pttByChannel = new Map([[1, { method: 'serial_dtr' }]]);
  assert.equal(pttDetectionBlockedReason([c1], pttByChannel), 'all-configured');
});
