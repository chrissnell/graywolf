import test from 'node:test';
import assert from 'node:assert/strict';
import { splitRFToISBlocklist, validateRFToISBlocklist } from './igateBlocklist.js';

test('multiple callsigns on separate lines are accepted', () => {
  const input = 'DB0SDA\nDL0GY';
  assert.deepEqual(splitRFToISBlocklist(input), ['DB0SDA', 'DL0GY']);
  assert.equal(validateRFToISBlocklist(input), '');
});

test('spaces, tabs, CRLF, comma and semicolon all separate callsigns', () => {
  for (const input of [
    'DB0SDA DL0GY',
    'DB0SDA\tDL0GY',
    'DB0SDA\r\nDL0GY',
    'DB0SDA,DL0GY',
    'DB0SDA; DL0GY',
  ]) {
    assert.deepEqual(splitRFToISBlocklist(input), ['DB0SDA', 'DL0GY'], input);
    assert.equal(validateRFToISBlocklist(input), '', input);
  }
});

test('empty list and valid specific SSIDs are allowed', () => {
  for (const input of ['', '\n', 'DB0SDA-2\nDL0GY-13', 'da6pg-5,DL0GY']) {
    assert.equal(validateRFToISBlocklist(input), '', input);
  }
});

test('bad callsigns, wildcards, SSID >15 and more than 100 entries are rejected', () => {
  for (const input of ['DL0GY-16', 'DL0GY-*', 'TOOLONGCALL', 'DL0GY/13']) {
    assert.match(validateRFToISBlocklist(input), /^Invalid callsign:/);
  }
  assert.equal(validateRFToISBlocklist(Array(101).fill('DL0GY').join('\n')), 'Maximum 100 callsigns.');
});

test('spaces separated callsigns cannot be treated as a single invalid token', () => {
  assert.equal(validateRFToISBlocklist('DB0SDA DL0GY'), '');
});
