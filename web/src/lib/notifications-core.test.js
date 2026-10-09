// Tests for the pure queue-management helpers behind the new-activity
// popup stack (notificationsStore.svelte.js wraps these with $state).
//
// Run with:
//   node --test src/lib/notifications-core.test.js

import { strict as assert } from 'node:assert';
import { describe, it } from 'node:test';

import { addEntry, removeEntry } from './notifications-core.js';

describe('addEntry', () => {
  it('appends the new entry to the end', () => {
    const entries = [{ id: 'a' }, { id: 'b' }];
    const next = addEntry(entries, { id: 'c' }, 4);
    assert.deepEqual(next.map((e) => e.id), ['a', 'b', 'c']);
  });

  it('does not mutate the input array', () => {
    const entries = [{ id: 'a' }];
    addEntry(entries, { id: 'b' }, 4);
    assert.equal(entries.length, 1);
  });

  it('caps the stack by dropping the oldest entries', () => {
    const entries = [{ id: 'a' }, { id: 'b' }, { id: 'c' }, { id: 'd' }];
    const next = addEntry(entries, { id: 'e' }, 4);
    assert.deepEqual(next.map((e) => e.id), ['b', 'c', 'd', 'e']);
    assert.equal(next.length, 4);
  });
});

describe('removeEntry', () => {
  it('filters out the entry with the matching id', () => {
    const entries = [{ id: 'a' }, { id: 'b' }, { id: 'c' }];
    const next = removeEntry(entries, 'b');
    assert.deepEqual(next.map((e) => e.id), ['a', 'c']);
  });

  it('is a no-op when the id is not present', () => {
    const entries = [{ id: 'a' }, { id: 'b' }];
    const next = removeEntry(entries, 'z');
    assert.deepEqual(next.map((e) => e.id), ['a', 'b']);
  });

  it('handles an empty array', () => {
    assert.deepEqual(removeEntry([], 'a'), []);
  });
});
