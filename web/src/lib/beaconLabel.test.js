import test from 'node:test';
import assert from 'node:assert/strict';
import { beaconLabel } from './beaconLabel.js';

test('beaconLabel: object beacon uses its object_name', () => {
  assert.equal(
    beaconLabel({ type: 'object', object_name: 'FIELDDAY', callsign: '' }, 'N0CAL'),
    'FIELDDAY',
  );
});

test('beaconLabel: object beacon without an object_name falls back to the callsign chain', () => {
  assert.equal(beaconLabel({ type: 'object', object_name: '', callsign: '' }, 'N0CAL'), 'N0CAL');
});

test('beaconLabel: non-object beacon prefers its per-row callsign override', () => {
  assert.equal(beaconLabel({ type: 'position', callsign: 'W1AW-9' }, 'N0CAL'), 'W1AW-9');
});

test('beaconLabel: empty per-row callsign falls back to the station callsign', () => {
  assert.equal(beaconLabel({ type: 'position', callsign: '' }, 'N0CAL'), 'N0CAL');
});

test('beaconLabel: with no callsign and no station callsign returns the (unset) sentinel', () => {
  assert.equal(beaconLabel({ type: 'position', callsign: '' }, ''), '(unset)');
  assert.equal(beaconLabel({ type: 'position', callsign: '' }), '(unset)');
});

test('beaconLabel: tolerates a null/undefined row', () => {
  assert.equal(beaconLabel(null, 'N0CAL'), 'N0CAL');
  assert.equal(beaconLabel(undefined), '(unset)');
});
