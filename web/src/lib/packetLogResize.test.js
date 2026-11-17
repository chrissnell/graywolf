import test from 'node:test';
import assert from 'node:assert/strict';
import {
  DEFAULT_LOG_HEIGHT, MIN_LOG_HEIGHT, MAX_LOG_HEIGHT, LOG_HEIGHT_KEY,
  normalizeLogHeight, maxLogHeight, draggedLogHeight, keyedLogHeight,
  readLogHeight, saveLogHeight, resizeEdgeStep,
} from './packetLogResize.js';

test('missing, malformed and nonfinite stored heights restore the default', () => {
  for (const value of [null, undefined, '', ' ', 'oops', '400px', NaN, Infinity, {}, true]) {
    assert.equal(normalizeLogHeight(value), DEFAULT_LOG_HEIGHT);
    assert.equal(readLogHeight({ getItem: () => value }), DEFAULT_LOG_HEIGHT);
  }
});

test('persisted heights are rounded and constrained to safe bounds', () => {
  assert.equal(normalizeLogHeight('-50'), MIN_LOG_HEIGHT);
  assert.equal(normalizeLogHeight('999999'), MAX_LOG_HEIGHT);
  assert.equal(normalizeLogHeight('640.6'), 641);
  let stored;
  saveLogHeight(720, { setItem: (key, value) => { stored = { key, value }; } });
  assert.deepEqual(stored, { key: LOG_HEIGHT_KEY, value: '720' });
  assert.equal(readLogHeight({ getItem: () => stored.value }), 720);
});

test('unavailable browser storage does not prevent reading or resizing', () => {
  const denied = { getItem() { throw Error('denied'); }, setItem() { throw Error('denied'); } };
  assert.equal(readLogHeight(denied), DEFAULT_LOG_HEIGHT);
  assert.doesNotThrow(() => saveLogHeight(800, denied));
  assert.equal(readLogHeight(), DEFAULT_LOG_HEIGHT);
  assert.doesNotThrow(() => saveLogHeight(800));
});

test('content bounds preserve the default and stop expansion at the content or safety cap', () => {
  assert.equal(maxLogHeight(0), DEFAULT_LOG_HEIGHT);
  assert.equal(maxLogHeight(250), DEFAULT_LOG_HEIGHT);
  assert.equal(maxLogHeight(735.2), 736);
  assert.equal(maxLogHeight(3000), MAX_LOG_HEIGHT);
  assert.equal(maxLogHeight(NaN), DEFAULT_LOG_HEIGHT);
});

test('pointer resize compensates for UI scale and cannot escape content bounds', () => {
  assert.equal(draggedLogHeight(400, 200, 2, 1000), 500);
  assert.equal(draggedLogHeight(400, 90, 0.9, 1000), 500);
  assert.equal(draggedLogHeight(400, -1000, 1, 1000), MIN_LOG_HEIGHT);
  assert.equal(draggedLogHeight(400, 2000, 1, 735), 735);
  assert.equal(draggedLogHeight(400, 100, 0, 1000), 500);
});

test('keyboard supports incremental resizing, limits, and normal Tab navigation', () => {
  assert.equal(keyedLogHeight('ArrowUp', 400, 900), 360);
  assert.equal(keyedLogHeight('ArrowDown', 400, 900), 440);
  assert.equal(keyedLogHeight('PageUp', 400, 900), 200);
  assert.equal(keyedLogHeight('PageDown', 400, 900), 600);
  assert.equal(keyedLogHeight('Home', 400, 900), MIN_LOG_HEIGHT);
  assert.equal(keyedLogHeight('End', 400, 900), 900);
  assert.equal(keyedLogHeight('ArrowDown', 900, 900), 900);
  assert.equal(keyedLogHeight('ArrowUp', 200, 900), 200);
  assert.equal(keyedLogHeight('Tab', 400, 900), null);
});

test('edge resizing continues with a stationary pointer and bounds long frames', () => {
  assert.equal(resizeEdgeStep(500, 1000, 16), 0);
  assert.equal(resizeEdgeStep(1000, 1000, 16), 7.68);
  assert.equal(resizeEdgeStep(0, 1000, 16), -7.68);
  assert.equal(resizeEdgeStep(1000, 1000, 10000), 15.36);
  assert.equal(resizeEdgeStep(976, 1000, 16), 3.84);
  assert.equal(resizeEdgeStep(1000, 1000, -5), 0);
});
