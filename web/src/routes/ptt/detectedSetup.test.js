import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { runInNewContext } from 'node:vm';
import { parse } from 'svelte/compiler';
import { DESKTOP_METHODS } from './devices/methodOptions.desktop.js';

// Execute the actual route handlers, with UI state and persistence mocked.
const source = readFileSync(new URL('../Ptt.svelte', import.meta.url), 'utf8');
const names = ['methodOptionForDevice', 'configureFromDetected', 'needsDevice',
  'handleMethodSaveAndNext', 'handleDeviceSave', 'handleDeviceBack'];
const handlers = parse(source).instance.content.body
  .filter(node => node.type === 'FunctionDeclaration' && names.includes(node.id.name))
  .map(node => source.slice(node.start, node.end)).join('\n');
function setup() {
  const context = {
    isAndroid: false, methodOptionsForPlatform: DESKTOP_METHODS,
    channels: [{ id: 2 }], channelsNeedingPttList: [{ id: 2 }],
    dialogContext: null, dialogMethodChosen: null,
    dialogDeviceOpen: false, dialogMethodOpen: false,
    errors: [], saves: [],
  };
  context.toasts = { error: message => context.errors.push(message) };
  context.persistFromDialogs = payload => context.saves.push({
    payload, method: context.dialogMethodChosen, context: context.dialogContext,
  });
  runInNewContext(handlers, context);
  return context;
}
for (const recommended of [false, true]) {
  test(`detected serial device (recommended=${recommended}) confirms method before activation`, () => {
    const c = setup();
    c.configureFromDetected({ type: 'serial', path: '/dev/ttyUSB0', recommended });
    assert.equal(c.dialogMethodOpen, true);
    assert.equal(c.dialogDeviceOpen, false);
    assert.equal(c.dialogContext.item.device_path, '/dev/ttyUSB0');
    assert.equal(c.saves.length, 0);
    const dual = DESKTOP_METHODS.find(m => m.wire.method === 'serial_rts_dtr');
    c.handleMethodSaveAndNext(dual, null);
    assert.equal(c.dialogMethodOpen, false);
    assert.equal(c.dialogDeviceOpen, true);
    assert.equal(c.saves.length, 0);
    c.handleDeviceBack();
    assert.equal(c.saves.length, 0);
    c.handleMethodSaveAndNext(dual, null);
    c.handleDeviceSave({ device: { path: '/dev/ttyUSB0' }, invert: true, dtr_invert: false });
    assert.equal(c.saves.length, 1);
    assert.equal(c.saves[0].method.wire.method, 'serial_rts_dtr');
    assert.equal(c.saves[0].payload.invert, true);
    assert.equal(c.saves[0].payload.dtr_invert, false);
  });
}
test('unknown detected device opens method selection without saving', () => {
  const c = setup();
  c.configureFromDetected({ type: 'unknown', path: '/dev/custom', recommended: true });
  assert.equal(c.dialogMethodChosen, null);
  assert.equal(c.dialogMethodOpen, true);
  assert.equal(c.saves.length, 0);
});
test('missing eligible channel refuses setup without activation', () => {
  const c = setup();
  c.channelsNeedingPttList = [];
  c.configureFromDetected({ type: 'serial', path: '/dev/ttyUSB0', recommended: true });
  assert.equal(c.errors.length, 1);
  assert.equal(c.dialogMethodOpen, false);
  assert.equal(c.saves.length, 0);
});
