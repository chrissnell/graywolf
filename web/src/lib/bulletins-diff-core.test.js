// Tests for the pure "newly unread" diffing helper used by
// bulletinsStore.svelte.js's replaceInbound() to decide which bulletins
// should raise a notification on each poll.
//
// Run with:
//   node --test src/lib/bulletins-diff-core.test.js

import { strict as assert } from 'node:assert';
import { describe, it } from 'node:test';

import { diffNewlyUnread } from './bulletins-diff-core.js';

function row(id, unread, updated_at = '2026-07-27T00:00:00Z') {
  return { id, unread, updated_at, from_call: `N0CALL-${id}`, slot: 'BLN0', text: 'hi' };
}

describe('diffNewlyUnread', () => {
  it('includes a brand-new unread row not present in the previous snapshot', () => {
    const prev = new Map();
    const fresh = [row(1, true)];
    assert.deepEqual(diffNewlyUnread(prev, fresh), fresh);
  });

  it('excludes an unchanged already-unread row (same updated_at)', () => {
    const prev = new Map([[1, row(1, true, 't1')]]);
    const fresh = [row(1, true, 't1')];
    assert.deepEqual(diffNewlyUnread(prev, fresh), []);
  });

  it('includes a row whose unread flag flipped back to true (re-heard after read)', () => {
    const prev = new Map([[1, row(1, false, 't1')]]);
    const fresh = [row(1, true, 't1')];
    assert.deepEqual(diffNewlyUnread(prev, fresh), fresh);
  });

  it('includes an already-unread row that was re-heard (updated_at moved)', () => {
    const prev = new Map([[1, row(1, true, 't1')]]);
    const fresh = [row(1, true, 't2')];
    assert.deepEqual(diffNewlyUnread(prev, fresh), fresh);
  });

  it('excludes rows that are not unread', () => {
    const prev = new Map();
    const fresh = [row(1, false)];
    assert.deepEqual(diffNewlyUnread(prev, fresh), []);
  });

  it('treats an empty previous snapshot as all-unread-rows-are-new', () => {
    const prev = new Map();
    const fresh = [row(1, true), row(2, false), row(3, true)];
    assert.deepEqual(diffNewlyUnread(prev, fresh), [fresh[0], fresh[2]]);
  });

  it('handles an empty fresh snapshot', () => {
    const prev = new Map([[1, row(1, true)]]);
    assert.deepEqual(diffNewlyUnread(prev, []), []);
    assert.deepEqual(diffNewlyUnread(prev, undefined), []);
  });
});
