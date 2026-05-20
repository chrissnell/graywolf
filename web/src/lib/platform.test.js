import { test, beforeEach, afterEach } from 'node:test';
import assert from 'node:assert/strict';
import { _resetForTests as resetBridge } from './androidBridge.js';
import { Platform } from './platform.js';

beforeEach(() => {
  resetBridge();
  delete globalThis.GraywolfWebInterface;
});
afterEach(() => {
  resetBridge();
  delete globalThis.GraywolfWebInterface;
});

test('Platform.kind === "desktop" when bridge absent', () => {
  assert.equal(Platform.kind, 'desktop');
});

test('Platform.kind === "android" when bridge present', () => {
  globalThis.GraywolfWebInterface = { getBearerToken: () => 'tok' };
  assert.equal(Platform.kind, 'android');
});

test('Platform.kind is read each access (dynamic)', () => {
  assert.equal(Platform.kind, 'desktop');
  globalThis.GraywolfWebInterface = { getBearerToken: () => 'tok' };
  assert.equal(Platform.kind, 'android');
  delete globalThis.GraywolfWebInterface;
  resetBridge();
  assert.equal(Platform.kind, 'desktop');
});

test('Platform.isAndroid === false when bridge absent', () => {
  assert.equal(Platform.isAndroid, false);
});

test('Platform.isAndroid === true when bridge present', () => {
  globalThis.GraywolfWebInterface = { getBearerToken: () => 'tok' };
  assert.equal(Platform.isAndroid, true);
});

test('Platform.isAndroid tracks bridge toggling dynamically', () => {
  assert.equal(Platform.isAndroid, false);
  globalThis.GraywolfWebInterface = { getBearerToken: () => 'tok' };
  assert.equal(Platform.isAndroid, true);
  delete globalThis.GraywolfWebInterface;
  resetBridge();
  assert.equal(Platform.isAndroid, false);
});

