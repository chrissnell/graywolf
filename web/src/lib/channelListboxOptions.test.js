import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  buildOptions,
  noneValueFor,
  asChannelNumber,
  resolveCurrentIdx,
} from './channelListboxOptions.js';

const CH = [
  { id: 1, name: 'VHF' },
  { id: 2, name: 'UHF' },
];

test('buildOptions without allowNone is one row per channel', () => {
  const opts = buildOptions(CH, false);
  assert.equal(opts.length, 2);
  assert.equal(opts[0].none, false);
  assert.equal(opts[0].channel.id, 1);
  assert.equal(opts[1].channel.id, 2);
});

test('buildOptions with allowNone prepends a none row', () => {
  const opts = buildOptions(CH, true);
  assert.equal(opts.length, 3);
  assert.equal(opts[0].none, true);
  assert.equal(opts[1].channel.id, 1);
});

test('buildOptions tolerates a null channel list', () => {
  assert.deepEqual(buildOptions(null, false), []);
  assert.equal(buildOptions(null, true).length, 1);
  assert.equal(buildOptions(undefined, true)[0].none, true);
});

test('noneValueFor maps to the unset sentinel per value space', () => {
  assert.equal(noneValueFor('number'), 0);
  assert.equal(noneValueFor('string'), '');
});

test('asChannelNumber coerces strings and rejects the unset cases', () => {
  assert.equal(asChannelNumber('2'), 2);
  assert.equal(asChannelNumber(2), 2);
  assert.equal(asChannelNumber(''), null);
  assert.equal(asChannelNumber(null), null);
  assert.equal(asChannelNumber(undefined), null);
  assert.equal(asChannelNumber('not-a-number'), null);
});

test('resolveCurrentIdx finds a channel row by numeric id (string or number)', () => {
  const opts = buildOptions(CH, false);
  assert.equal(resolveCurrentIdx(opts, 2), 1);
  assert.equal(resolveCurrentIdx(opts, '2'), 1);
});

test('resolveCurrentIdx maps value 0 to the none row when allowNone', () => {
  const opts = buildOptions(CH, true);
  // none row is index 0; value 0 / null / '' all select it.
  assert.equal(resolveCurrentIdx(opts, 0), 0);
  assert.equal(resolveCurrentIdx(opts, null), 0);
  assert.equal(resolveCurrentIdx(opts, ''), 0);
  // a real channel still resolves past the none row's offset.
  assert.equal(resolveCurrentIdx(opts, 1), 1);
  assert.equal(resolveCurrentIdx(opts, 2), 2);
});

test('resolveCurrentIdx returns -1 for value 0 without a none row (placeholder)', () => {
  // The graywolf#396 regression guard: when the page does NOT offer a
  // none row, an unset (0) value must fall through to the placeholder,
  // not silently bind to a real channel.
  const opts = buildOptions(CH, false);
  assert.equal(resolveCurrentIdx(opts, 0), -1);
  assert.equal(resolveCurrentIdx(opts, null), -1);
});

test('resolveCurrentIdx returns -1 for an unknown channel id', () => {
  const opts = buildOptions(CH, true);
  assert.equal(resolveCurrentIdx(opts, 99), -1);
});
