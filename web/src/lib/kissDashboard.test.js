import test from 'node:test';
import assert from 'node:assert/strict';
import { kissLinkStatus, kissTransportLabel } from './kissDashboard.js';

test('a connected idle TNC remains connected without packet counters', () => {
  assert.equal(kissLinkStatus({ enabled: true, state: 'connected' }).tone, 'connected');
});
test('failed status refresh never displays a stale connected link', () => {
  assert.equal(kissLinkStatus({ enabled: true, state: 'connected' }, false).tone, 'unknown');
});
test('disabled interfaces override retained supervisor state', () => {
  assert.equal(kissLinkStatus({ enabled: false, state: 'connected' }).label, 'Disabled');
});
test('connection attempts and reconnect backoff remain pending', () => {
  for (const state of ['connecting', 'backoff', 'listening']) {
    assert.equal(kissLinkStatus({ enabled: true, state }).tone, 'pending');
  }
});
test('stopped or absent supervisors never imply a connection', () => {
  for (const state of ['stopped', 'disconnected', '', undefined]) {
    assert.equal(kissLinkStatus({ enabled: true, state }).tone, 'disconnected');
  }
});
test('BLE labels describe the link instead of inventing an RF modulation', () => {
  assert.equal(kissTransportLabel('ble-device'), 'BLE');
});
