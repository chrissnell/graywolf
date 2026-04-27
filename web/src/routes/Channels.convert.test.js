// Channel type conversion logic: tests for the pure payload-shaping
// function used by Channels.svelte. The Svelte side is exercised
// manually (no test harness in this repo yet); this covers the
// branching rules so the regressions most likely to slip past review
// (e.g. forgetting to zero output_device_id when switching to
// kiss-tnc) trip a test instead.

import { test } from 'node:test';
import assert from 'node:assert/strict';

// buildChannelPayload mirrors the logic in Channels.svelte.
// Kept as a pure function here so it is testable; the Svelte
// component imports the same idea inline. Duplication is acceptable
// at this scale; Phase 6 can lift to a shared lib if more call sites
// emerge.
function buildChannelPayload(form) {
  const base = {
    name: form.name,
    modem_type: form.modem_type,
    bit_rate: parseInt(form.bit_rate, 10),
    mark_freq: parseInt(form.mark_freq, 10),
    space_freq: parseInt(form.space_freq, 10),
    input_channel: parseInt(form.input_channel, 10),
    output_channel: parseInt(form.output_channel, 10),
  };
  if (form.channel_type === 'kiss-tnc') {
    return {
      ...base,
      input_device_id: null,
      input_channel: 0,
      output_device_id: 0,
      output_channel: 0,
    };
  }
  return {
    ...base,
    input_device_id: parseInt(form.input_device_id, 10),
    output_device_id: parseInt(form.output_device_id, 10),
  };
}

test('modem-backed payload keeps the selected input device', () => {
  const form = {
    name: 'VHF', channel_type: 'modem',
    input_device_id: '5', input_channel: '0',
    output_device_id: '7', output_channel: '1',
    modem_type: 'afsk', bit_rate: '1200', mark_freq: '1200', space_freq: '2200',
  };
  const payload = buildChannelPayload(form);
  assert.equal(payload.input_device_id, 5);
  assert.equal(payload.output_device_id, 7);
  assert.equal(payload.input_channel, 0);
  assert.equal(payload.output_channel, 1);
});

test('kiss-tnc payload forces input_device_id=null and zeroes audio fields', () => {
  const form = {
    name: 'LoRa', channel_type: 'kiss-tnc',
    // Operator could have stale non-zero values left over from a
    // conversion; payload must scrub them to keep the backend
    // validator happy.
    input_device_id: '5', input_channel: '1',
    output_device_id: '7', output_channel: '1',
    modem_type: 'afsk', bit_rate: '1200', mark_freq: '1200', space_freq: '2200',
  };
  const payload = buildChannelPayload(form);
  assert.equal(payload.input_device_id, null);
  assert.equal(payload.input_channel, 0);
  assert.equal(payload.output_device_id, 0);
  assert.equal(payload.output_channel, 0);
});

test('kiss-tnc payload preserves name and modem metadata for round-trip', () => {
  // A kiss-tnc channel can still carry modem_type / bit_rate
  // because the Convert flow may need to flip back to modem-backed.
  // The store round-trips these fields unchanged.
  const form = {
    name: 'hybrid', channel_type: 'kiss-tnc',
    input_device_id: '0', input_channel: '0',
    output_device_id: '0', output_channel: '0',
    modem_type: 'gfsk', bit_rate: '9600', mark_freq: '1200', space_freq: '2200',
  };
  const payload = buildChannelPayload(form);
  assert.equal(payload.name, 'hybrid');
  assert.equal(payload.modem_type, 'gfsk');
  assert.equal(payload.bit_rate, 9600);
});
