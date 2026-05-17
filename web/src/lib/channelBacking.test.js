import test from 'node:test';
import assert from 'node:assert/strict';
import {
  tooltipText,
  SUMMARY_MODEM,
  SUMMARY_KISS_TNC,
} from './channelBacking.js';

test('tooltipText: kiss-tnc summary joins kiss errors, never modem reason', () => {
  const backing = {
    summary: SUMMARY_KISS_TNC,
    modem: { reason: 'no audio input device' },
    kiss_tnc: [
      { last_error: 'serial open failed: permission denied' },
      { last_error: '' },
      { last_error: 'device closed' },
    ],
  };
  assert.equal(
    tooltipText(backing),
    'serial open failed: permission denied; device closed',
  );
});

test('tooltipText: kiss-tnc summary with no errors is empty', () => {
  const backing = {
    summary: SUMMARY_KISS_TNC,
    modem: { reason: 'no audio input device' },
    kiss_tnc: [{ last_error: '' }],
  };
  assert.equal(tooltipText(backing), '');
});

test('tooltipText: modem summary returns modem reason', () => {
  const backing = {
    summary: SUMMARY_MODEM,
    modem: { reason: 'modem subprocess not running' },
    kiss_tnc: [],
  };
  assert.equal(tooltipText(backing), 'modem subprocess not running');
});

test('tooltipText: unbound / unknown summary is empty', () => {
  assert.equal(tooltipText({ summary: 'unbound', modem: { reason: 'x' } }), '');
  assert.equal(tooltipText(null), '');
  assert.equal(tooltipText({}), '');
  assert.equal(tooltipText({ summary: SUMMARY_KISS_TNC }), '');
  assert.equal(tooltipText({ summary: SUMMARY_MODEM }), '');
});
