import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  methodLabel,
  summaryLine,
  pttState,
  ariaLabel,
  METHOD_NONE,
} from './channelPtt.js';

test('methodLabel translates known methods and falls back to raw', () => {
  assert.equal(methodLabel(METHOD_NONE), 'None');
  assert.equal(methodLabel('cm108'), 'CM108');
  assert.equal(methodLabel('serial_rts'), 'Serial RTS');
  assert.equal(methodLabel('rigctld'), 'rigctld');
  assert.equal(methodLabel('digirig_tone'), 'Digirig Lite Tone PTT');
  assert.equal(methodLabel(''), 'None');
  assert.equal(methodLabel(undefined), 'None');
  // Unknown method -> degrade to raw token rather than blank.
  assert.equal(methodLabel('newmethod'), 'newmethod');
});

test('summaryLine surfaces method + detail when configured', () => {
  assert.equal(
    summaryLine({ method: 'cm108', configured: true, detail: 'GPIO 3 · /dev/hidraw0' }),
    'CM108 · GPIO 3 · /dev/hidraw0',
  );
  assert.equal(
    summaryLine({ method: 'serial_rts', configured: true, detail: '/dev/ttyUSB0' }),
    'Serial RTS · /dev/ttyUSB0',
  );
  assert.equal(
    summaryLine({ method: 'serial_rts', configured: true, detail: '' }),
    'Serial RTS',
  );
});

test('summaryLine renders method-only state when not configured', () => {
  assert.equal(summaryLine({ method: 'none', configured: false }), 'None');
});

test('summaryLine reports "Not configured" when ptt is missing', () => {
  assert.equal(summaryLine(null), 'Not configured');
  assert.equal(summaryLine(undefined), 'Not configured');
});

test('pttState mirrors backing tri-state', () => {
  assert.equal(pttState(null), 'unbound');
  assert.equal(pttState({ method: 'none', configured: false }), 'down');
  assert.equal(pttState({ method: 'cm108', configured: true }), 'live');
});

test('ariaLabel is screen-reader friendly across states', () => {
  assert.equal(ariaLabel(null), 'PTT not configured');
  assert.equal(ariaLabel({ method: 'none', configured: false }), 'PTT method none');
  assert.equal(
    ariaLabel({ method: 'cm108', configured: true, detail: 'GPIO 3' }),
    'PTT CM108, GPIO 3',
  );
  assert.equal(
    ariaLabel({ method: 'serial_dtr', configured: true }),
    'PTT Serial DTR',
  );
});
