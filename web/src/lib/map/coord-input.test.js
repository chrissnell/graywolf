import test from 'node:test';
import assert from 'node:assert/strict';
import { parseCoordinate } from './coord-input.js';

test('parses plain decimal degrees', () => {
  assert.deepEqual(parseCoordinate('37.7749', 'lat'), { value: 37.7749 });
  assert.deepEqual(parseCoordinate('-122.4194', 'lon'), { value: -122.4194 });
});

test('honors hemisphere letters', () => {
  assert.deepEqual(parseCoordinate('37.7749 N', 'lat'), { value: 37.7749 });
  assert.deepEqual(parseCoordinate('37.7749S', 'lat'), { value: -37.7749 });
  assert.deepEqual(parseCoordinate('W122.4194', 'lon'), { value: -122.4194 });
});

test('accepts integers and bare decimals', () => {
  assert.deepEqual(parseCoordinate('0', 'lat'), { value: 0 });
  assert.deepEqual(parseCoordinate('.5', 'lon'), { value: 0.5 });
});

test('rejects empty input', () => {
  assert.match(parseCoordinate('   ', 'lat').error, /required/);
});

test('rejects out-of-range values', () => {
  assert.match(parseCoordinate('91', 'lat').error, /between/);
  assert.match(parseCoordinate('181', 'lon').error, /between/);
  assert.equal(parseCoordinate('90', 'lat').value, 90);
  assert.equal(parseCoordinate('-180', 'lon').value, -180);
});

test('rejects wrong-axis hemisphere letters', () => {
  assert.match(parseCoordinate('37.0 E', 'lat').error, /N or S/);
  assert.match(parseCoordinate('122.0 N', 'lon').error, /E or W/);
});

test('rejects a minus sign combined with a hemisphere letter', () => {
  assert.match(parseCoordinate('-37.0 S', 'lat').error, /minus sign/);
});

test('rejects non-numeric junk', () => {
  assert.match(parseCoordinate('abc', 'lat').error, /valid number/);
  assert.match(parseCoordinate('12.3.4', 'lon').error, /valid number/);
});
