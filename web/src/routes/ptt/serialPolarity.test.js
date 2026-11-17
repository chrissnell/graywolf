import { test } from 'node:test';
import assert from 'node:assert/strict';
import { serialFlags, serialOptions, serialValue } from './serialPolarity.js';
test('all dual-line selections preserve flags and show correct TX/RX levels', () => {
  const options = serialOptions('serial_rts_dtr');
  assert.equal(options.length, 4);
  for (const invert of [false, true]) for (const dtr_invert of [false, true]) {
    const value = serialValue('serial_rts_dtr', invert, dtr_invert);
    assert.deepEqual(serialFlags('serial_rts_dtr', value), { invert, dtr_invert });
    const option = options.find(o => o.value === value);
    assert.ok(option.label.includes(`RTS: TX ${invert ? 'LOW / RX HIGH' : 'HIGH / RX LOW'}`));
    assert.ok(option.label.includes(`DTR: TX ${dtr_invert ? 'LOW / RX HIGH' : 'HIGH / RX LOW'}`));
  }
});
test('single-line selections retain invert and ignore unused DTR polarity', () => {
  for (const method of ['serial_rts', 'serial_dtr']) {
    const options = serialOptions(method);
    assert.equal(options.length, 2);
    for (const invert of [false, true]) {
      const value = serialValue(method, invert, true);
      assert.deepEqual(serialFlags(method, value), { invert, dtr_invert: false });
      assert.ok(options.some(o => o.value === value));
      assert.ok(options.every(o => o.label.startsWith(method === 'serial_rts' ? 'RTS:' : 'DTR:')));
    }
  }
});
