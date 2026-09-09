import test from 'node:test';
import assert from 'node:assert/strict';
import { beaconChannelDisplay, AUTO_CHANNEL_LABEL } from './beaconChannelDisplay.js';

function capableChannel(id, name = `ch${id}`) {
  return { id, name, backing: { tx: { capable: true, reason: '' } } };
}

function incapableChannel(id, reason, name = `ch${id}`) {
  return { id, name, backing: { tx: { capable: false, reason } } };
}

test('beaconChannelDisplay: is_only beacon ignores channel entirely', () => {
  const map = new Map([[1, capableChannel(1, 'VHF')]]);
  // channel=1 here would normally resolve fine, but send_path=is_only
  // must win regardless of what channel holds.
  const result = beaconChannelDisplay({ channel: 1, send_path: 'is_only' }, map);
  assert.equal(result.mode, 'is_only');
  assert.equal(result.label, 'Send to');
  assert.equal(result.value, 'APRS-IS only (no radio)');
  assert.equal(result.showValue, true);
  assert.equal(result.broken, false);
});

test('beaconChannelDisplay: channel=0 on an rf beacon is Auto', () => {
  const result = beaconChannelDisplay({ channel: 0, send_path: 'rf' }, new Map());
  assert.equal(result.mode, 'auto');
  assert.equal(result.label, 'Channel');
  assert.equal(result.value, AUTO_CHANNEL_LABEL);
  assert.equal(result.showValue, true);
  assert.equal(result.broken, false);
});

test('beaconChannelDisplay: channel=0 on a both beacon is also Auto', () => {
  const result = beaconChannelDisplay({ channel: 0, send_path: 'both' }, new Map());
  assert.equal(result.mode, 'auto');
  assert.equal(result.value, AUTO_CHANNEL_LABEL);
});

test('beaconChannelDisplay: a TX-capable channel reports ok', () => {
  const map = new Map([[3, capableChannel(3, 'VHF')]]);
  const result = beaconChannelDisplay({ channel: 3, send_path: 'rf' }, map);
  assert.equal(result.mode, 'ok');
  assert.equal(result.label, 'Channel');
  assert.equal(result.value, 'VHF');
  assert.equal(result.showValue, true);
  assert.equal(result.broken, false);
  assert.equal(result.ariaLabel, 'Channel VHF');
});

test('beaconChannelDisplay: an existing but non-TX-capable channel reports unreachable with the server reason', () => {
  const map = new Map([[5, incapableChannel(5, 'no output device configured', 'UHF')]]);
  const result = beaconChannelDisplay({ channel: 5, send_path: 'rf' }, map);
  assert.equal(result.mode, 'unreachable');
  assert.equal(result.label, 'Unreachable: no output device configured');
  assert.equal(result.value, 'UHF');
  assert.equal(result.showValue, true);
  assert.equal(result.broken, true);
  assert.equal(result.ariaLabel, 'UHF unreachable: no output device configured');
});

test('beaconChannelDisplay: a channel id missing from the store reports deleted and hides the value', () => {
  const map = new Map([[1, capableChannel(1)]]);
  const result = beaconChannelDisplay({ channel: 99, send_path: 'rf' }, map);
  assert.equal(result.mode, 'deleted');
  assert.equal(result.label, 'Channel deleted');
  assert.equal(result.value, undefined);
  assert.equal(result.showValue, false);
  assert.equal(result.broken, true);
  assert.equal(result.ariaLabel, 'Channel #99 deleted');
});
