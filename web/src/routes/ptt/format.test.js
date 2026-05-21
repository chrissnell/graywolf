// web/src/routes/ptt/format.test.js
import { test } from 'node:test';
import assert from 'node:assert/strict';

import { truncatePath } from './format.js';

test('truncatePath renders the em-dash placeholder for empty input', () => {
  assert.equal(truncatePath(''), '—');
  assert.equal(truncatePath(null), '—');
  assert.equal(truncatePath(undefined), '—');
});

test('truncatePath passes through a string shorter than max unchanged', () => {
  assert.equal(truncatePath('/dev/ttyUSB0'), '/dev/ttyUSB0');
});

test('truncatePath ellipses long strings from the front, preserving the tail', () => {
  const long = '/very/long/path/segments/that/exceed/the/limit/ttyUSB0';
  const out = truncatePath(long);
  assert.equal(out.length, 40);
  assert.ok(out.startsWith('...'));
  assert.ok(out.endsWith('ttyUSB0'));
});
