import test from 'node:test';
import assert from 'node:assert/strict';

import { localEcho } from './localecho.js';

test('echoes printable characters unchanged', () => {
  assert.equal(localEcho('CONNECT W1AW'), 'CONNECT W1AW');
});

test('maps a typed CR (Enter) to CRLF so the cursor advances', () => {
  assert.equal(localEcho('\r'), '\r\n');
});

test('maps a bare LF to CRLF as well', () => {
  assert.equal(localEcho('\n'), '\r\n');
});

test('rubs out the previous glyph on backspace (DEL)', () => {
  assert.equal(localEcho('\x7f'), '\b \b');
});

test('rubs out on a literal BS too', () => {
  assert.equal(localEcho('\b'), '\b \b');
});

test('passes a tab through for xterm to expand', () => {
  assert.equal(localEcho('\t'), '\t');
});

test('drops stray C0 control characters', () => {
  assert.equal(localEcho('\x01\x02'), '');
});

test('skips the whole chunk for an escape sequence (arrow key)', () => {
  assert.equal(localEcho('\x1b[A'), '');
});

test('echoes pasted multi-line command text, CRs promoted', () => {
  assert.equal(localEcho('LIST\rREAD 1\r'), 'LIST\r\nREAD 1\r\n');
});

test('preserves multi-byte UTF-8 characters', () => {
  assert.equal(localEcho('grü'), 'grü');
});

test('empty input echoes nothing', () => {
  assert.equal(localEcho(''), '');
});
