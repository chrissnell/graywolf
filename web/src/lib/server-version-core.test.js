import { test } from 'node:test';
import assert from 'node:assert/strict';
import { serverIdentity, createIdentityWatcher } from './server-version-core.js';

// serverIdentity joins (version, commit) with a pipe separator.
const J = (v, c) => v + '|' + c;

test('serverIdentity combines version and commit', () => {
  assert.equal(serverIdentity({ version: '0.14.9', commit: 'abc123' }), J('0.14.9', 'abc123'));
});

test('serverIdentity tolerates missing fields', () => {
  assert.equal(serverIdentity({ version: '0.14.9' }), J('0.14.9', ''));
  assert.equal(serverIdentity({ commit: 'abc123' }), J('', 'abc123'));
});

test('serverIdentity returns empty for blank/missing input', () => {
  assert.equal(serverIdentity(null), '');
  assert.equal(serverIdentity(undefined), '');
  assert.equal(serverIdentity({}), '');
  assert.equal(serverIdentity('nope'), '');
  assert.equal(serverIdentity({ version: '', commit: '' }), '');
});

test('serverIdentity distinguishes same version, different commit', () => {
  assert.notEqual(
    serverIdentity({ version: '0.14.9', commit: 'aaa' }),
    serverIdentity({ version: '0.14.9', commit: 'bbb' }),
  );
});

test('watcher records boot identity without reporting a change', () => {
  const w = createIdentityWatcher();
  assert.equal(w.observe(J('0.14.9', 'abc')), false);
  assert.equal(w.boot, J('0.14.9', 'abc'));
  assert.equal(w.changed, false);
});

test('watcher reports change exactly once on a different identity', () => {
  const w = createIdentityWatcher();
  w.observe(J('0.14.9', 'abc'));
  assert.equal(w.observe(J('0.14.9', 'abc')), false, 'same identity is not a change');
  assert.equal(w.observe(J('0.15.0', 'def')), true, 'first differing identity flips the latch');
  assert.equal(w.observe(J('0.15.0', 'def')), false, 'does not re-fire for the same new identity');
  assert.equal(w.observe(J('0.16.0', 'ghi')), false, 'stays latched, no second fire');
  assert.equal(w.changed, true);
});

test('watcher ignores empty observations (transient failures)', () => {
  const w = createIdentityWatcher();
  assert.equal(w.observe(''), false, 'empty before boot does not establish baseline');
  assert.equal(w.boot, '');
  w.observe(J('0.14.9', 'abc'));
  assert.equal(w.observe(''), false, 'empty after boot is not a change');
  assert.equal(w.changed, false);
  assert.equal(w.observe(J('0.15.0', 'def')), true);
});

test('watcher detects a same-version rebuild via commit', () => {
  const w = createIdentityWatcher();
  w.observe(serverIdentity({ version: '0.14.9', commit: 'aaa' }));
  assert.equal(w.observe(serverIdentity({ version: '0.14.9', commit: 'bbb' })), true);
});
