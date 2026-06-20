import { test } from 'node:test';
import assert from 'node:assert/strict';
import { get } from 'svelte/store';
import { online, markConnected, markDisconnected } from './connection.js';

test('starts optimistic (online) so first paint has no disconnect flash', () => {
  assert.equal(get(online), true);
});

test('markDisconnected / markConnected flip the shared online flag', () => {
  markDisconnected();
  assert.equal(get(online), false);
  markConnected();
  assert.equal(get(online), true);
});
