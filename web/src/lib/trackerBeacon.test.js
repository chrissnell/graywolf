import test from 'node:test';
import assert from 'node:assert/strict';
import { trackerBeaconFlags } from './trackerBeacon.js';

test('trackerBeaconFlags: tracker forces GPS and SmartBeaconing on', () => {
  assert.deepEqual(trackerBeaconFlags('tracker', 'gps'), { use_gps: true, smart_beacon: true });
});

test('trackerBeaconFlags: tracker forces GPS even if pos_source says fixed', () => {
  // The form forces pos_source to gps for trackers, but the payload must
  // be correct even if that state were stale — the backend builder reads
  // course/speed from the live fix and has no fixed-coordinate path.
  assert.deepEqual(trackerBeaconFlags('tracker', 'fixed'), { use_gps: true, smart_beacon: true });
});

test('trackerBeaconFlags: position honors GPS source without smart_beacon', () => {
  assert.deepEqual(trackerBeaconFlags('position', 'gps'), { use_gps: true, smart_beacon: false });
});

test('trackerBeaconFlags: position with fixed coordinates opts out of both', () => {
  assert.deepEqual(trackerBeaconFlags('position', 'fixed'), { use_gps: false, smart_beacon: false });
});

test('trackerBeaconFlags: switching a tracker back to object clears smart_beacon', () => {
  // Guards the no-stale-flag behavior: a beacon converted away from
  // tracker must not keep smart_beacon set, or the scheduler would still
  // treat it as a SmartBeaconing candidate.
  assert.deepEqual(trackerBeaconFlags('object', 'fixed'), { use_gps: false, smart_beacon: false });
});
