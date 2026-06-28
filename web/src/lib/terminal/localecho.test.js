import test from 'node:test';
import assert from 'node:assert/strict';

import { echoForDisplay } from './localecho.js';

test('echoes printable characters verbatim', () => {
  assert.equal(echoForDisplay('CONNECT'), 'CONNECT');
});

test('echoes non-ASCII printables verbatim', () => {
  assert.equal(echoForDisplay('grüße'), 'grüße');
});

test('expands CR to CRLF so Enter advances a row', () => {
  assert.equal(echoForDisplay('\r'), '\r\n');
});

test('expands LF to CRLF', () => {
  assert.equal(echoForDisplay('\n'), '\r\n');
});

test('rubs out the cell on backspace (DEL)', () => {
  assert.equal(echoForDisplay('\x7f'), '\b \b');
});

test('rubs out the cell on backspace (BS)', () => {
  assert.equal(echoForDisplay('\b'), '\b \b');
});

test('drops escape sequences like arrow keys', () => {
  assert.equal(echoForDisplay('\x1b[C'), '');
  assert.equal(echoForDisplay('\x1bOP'), '');
});

test('drops bare control bytes', () => {
  assert.equal(echoForDisplay('\x03'), ''); // Ctrl-C
});

test('handles a mixed line of typing plus Enter', () => {
  assert.equal(echoForDisplay('B W0XYZ\r'), 'B W0XYZ\r\n');
});

test('returns empty for empty input', () => {
  assert.equal(echoForDisplay(''), '');
});
