import { test } from 'node:test';
import assert from 'node:assert/strict';
import { blankForm, rowToForm, formToPayload, validateForm } from './channelForm.js';

test('blankForm has modem defaults', () => {
  const f = blankForm();
  assert.equal(f.tx_delay_ms, '300');
  assert.equal(f.input_device_id, '0');
});

test('rowToForm maps a kiss-tnc row (null input_device_id) to channel_type kiss-tnc', () => {
  const f = rowToForm({ id: 5, name: 'K', input_device_id: null, output_device_id: 0, input_channel: 0, output_channel: 0, bit_rate: 1200, mark_freq: 1200, space_freq: 2200 }, {});
  assert.equal(f.channel_type, 'kiss-tnc');
  assert.equal(f.input_device_id, '0');
});

test('formToPayload sends input_device_id null for kiss-tnc', () => {
  const p = formToPayload({ ...blankForm(), channel_type: 'kiss-tnc', name: 'X' });
  assert.equal(p.input_device_id, null);
});

test('blankForm channel_type is modem', () => {
  assert.equal(blankForm().channel_type, 'modem');
});

test('rowToForm maps a modem row (non-null input_device_id)', () => {
  const f = rowToForm({
    id: 3, name: 'VHF', input_device_id: 7, output_device_id: 8,
    input_channel: 0, output_channel: 1,
    bit_rate: 1200, mark_freq: 1200, space_freq: 2200,
  }, { tx_delay_ms: 250, tx_tail_ms: 50, slot_ms: 200, persist: 127, full_dup: true });
  assert.equal(f.channel_type, 'modem');
  assert.equal(f.input_device_id, '7');
  assert.equal(f.output_device_id, '8');
  assert.equal(f.tx_delay_ms, '250');
  assert.equal(f.persist, '127');
  assert.equal(f.full_dup, true);
});

test('rowToForm uses fallback timing defaults when timing is missing', () => {
  const f = rowToForm({
    id: 1, name: 'T', input_device_id: 2, output_device_id: 0,
    input_channel: 0, output_channel: 0,
    bit_rate: 1200, mark_freq: 1200, space_freq: 2200,
  }, null);
  assert.equal(f.tx_delay_ms, '300');
  assert.equal(f.tx_tail_ms, '100');
  assert.equal(f.slot_ms, '100');
  assert.equal(f.persist, '63');
  assert.equal(f.full_dup, false);
});

test('formToPayload modem: parses numeric fields', () => {
  const p = formToPayload({
    ...blankForm(),
    channel_type: 'modem',
    name: 'VHF',
    input_device_id: '5',
    output_device_id: '7',
    input_channel: '0',
    output_channel: '1',
  });
  assert.equal(p.input_device_id, 5);
  assert.equal(p.output_device_id, 7);
  assert.equal(p.input_channel, 0);
  assert.equal(p.output_channel, 1);
});

test('formToPayload kiss-tnc: zeroes audio fields', () => {
  const p = formToPayload({
    ...blankForm(),
    channel_type: 'kiss-tnc',
    name: 'LoRa',
    input_device_id: '5',
    input_channel: '1',
    output_device_id: '7',
    output_channel: '1',
  });
  assert.equal(p.input_device_id, null);
  assert.equal(p.input_channel, 0);
  assert.equal(p.output_device_id, 0);
  assert.equal(p.output_channel, 0);
});

test('validateForm: missing name returns error', () => {
  const errs = validateForm({ ...blankForm(), name: '' });
  assert.ok('name' in errs);
});

test('validateForm: modem type with no input_device_id returns error', () => {
  const errs = validateForm({ ...blankForm(), channel_type: 'modem', name: 'X', input_device_id: '0' });
  assert.ok('input_device_id' in errs);
});

test('validateForm: kiss-tnc with no input_device_id is valid', () => {
  const errs = validateForm({ ...blankForm(), channel_type: 'kiss-tnc', name: 'X', input_device_id: '0' });
  assert.ok(!('input_device_id' in errs));
});

test('validateForm: persist out of range when tx enabled', () => {
  const errs = validateForm({
    ...blankForm(),
    channel_type: 'modem', name: 'X',
    input_device_id: '5', output_device_id: '7',
    persist: '999',
  });
  assert.ok('persist' in errs);
});

test('validateForm: valid modem form returns empty errors', () => {
  const errs = validateForm({
    ...blankForm(),
    channel_type: 'modem', name: 'VHF',
    input_device_id: '5',
  });
  assert.deepEqual(errs, {});
});
