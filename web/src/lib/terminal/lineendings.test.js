import test from 'node:test';
import assert from 'node:assert/strict';

import { createEolNormalizer } from './lineendings.js';

const enc = (s) => new Uint8Array([...s].map((c) => c.charCodeAt(0)));
const dec = (u) => String.fromCharCode(...u);

test('promotes bare CR to CRLF so lines advance', () => {
  const n = createEolNormalizer();
  assert.equal(dec(n(enc('line1\rline2\r'))), 'line1\r\nline2\r\n');
});

test('promotes bare LF to CRLF', () => {
  const n = createEolNormalizer();
  assert.equal(dec(n(enc('line1\nline2\n'))), 'line1\r\nline2\r\n');
});

test('leaves existing CRLF untouched (no doubling)', () => {
  const n = createEolNormalizer();
  assert.equal(dec(n(enc('line1\r\nline2\r\n'))), 'line1\r\nline2\r\n');
});

test('handles mixed terminators in one chunk', () => {
  const n = createEolNormalizer();
  assert.equal(dec(n(enc('a\rb\nc\r\nd'))), 'a\r\nb\r\nc\r\nd');
});

test('keeps a CRLF split across two chunks as a single break', () => {
  const n = createEolNormalizer();
  let out = dec(n(enc('abc\r')));
  out += dec(n(enc('\ndef')));
  assert.equal(out, 'abc\r\ndef');
});

test('two bare CRs across a chunk boundary stay two breaks', () => {
  const n = createEolNormalizer();
  let out = dec(n(enc('x\r')));
  out += dec(n(enc('\ry')));
  assert.equal(out, 'x\r\n\r\ny');
});

test('does not touch non-EOL bytes, including high bytes', () => {
  const n = createEolNormalizer();
  const input = new Uint8Array([0x48, 0x69, 0x80, 0xff]);
  assert.deepEqual([...n(input)], [0x48, 0x69, 0x80, 0xff]);
});

test('empty and nullish chunks are safe', () => {
  const n = createEolNormalizer();
  assert.equal(n(new Uint8Array(0)).length, 0);
  assert.equal(n(null).length, 0);
});

test('a lone trailing CR is not held back', () => {
  const n = createEolNormalizer();
  // The CR must render immediately rather than waiting for the next
  // chunk, otherwise the last received line would not advance.
  assert.equal(dec(n(enc('prompt\r'))), 'prompt\r\n');
});
