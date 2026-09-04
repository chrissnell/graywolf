// Tests for the pure IS/RF-echo collapsing helper used by
// MessageThread.svelte to display one bubble (with merged source
// badges) for the same packet heard on multiple paths.
//
// Run with:
//   node --test src/lib/duplicate-echo-core.test.js

import { strict as assert } from 'node:assert';
import { describe, it } from 'node:test';

import { collapseDuplicateEchoes } from './duplicate-echo-core.js';

function msg(id, overrides = {}) {
  return {
    id,
    direction: 'in',
    from_call: 'WXBOT',
    text: 'High 91',
    source: 'rf',
    sent_at: '2026-07-27T15:06:00Z',
    unread: true,
    ...overrides,
  };
}

describe('collapseDuplicateEchoes', () => {
  it('leaves a single message untouched, tagging it with its own source', () => {
    const out = collapseDuplicateEchoes([msg(1)]);
    assert.equal(out.length, 1);
    assert.deepEqual(out[0].mergedIds, [1]);
    assert.deepEqual(out[0].mergedSources, ['rf']);
  });

  it('merges an IS + RF echo of the same text heard seconds apart', () => {
    const out = collapseDuplicateEchoes([
      msg(1, { source: 'is', sent_at: '2026-07-27T15:06:00Z' }),
      msg(2, { source: 'rf', sent_at: '2026-07-27T15:06:02Z' }),
    ]);
    assert.equal(out.length, 1);
    assert.deepEqual(out[0].mergedIds, [1, 2]);
    assert.deepEqual(out[0].mergedSources, ['is', 'rf']);
  });

  it('does not merge across a gap wider than the window', () => {
    const out = collapseDuplicateEchoes([
      msg(1, { source: 'is', sent_at: '2026-07-27T15:06:00Z' }),
      msg(2, { source: 'rf', sent_at: '2026-07-27T15:06:30Z' }),
    ], { windowMs: 10_000 });
    assert.equal(out.length, 2);
  });

  it('does not merge different text from the same sender', () => {
    const out = collapseDuplicateEchoes([
      msg(1, { text: 'High 91' }),
      msg(2, { text: 'Low 75', sent_at: '2026-07-27T15:06:02Z' }),
    ]);
    assert.equal(out.length, 2);
  });

  it('does not merge the same text from a different sender', () => {
    const out = collapseDuplicateEchoes([
      msg(1, { from_call: 'WXBOT' }),
      msg(2, { from_call: 'K0TFU', sent_at: '2026-07-27T15:06:02Z' }),
    ]);
    assert.equal(out.length, 2);
  });

  it('never merges outbound bubbles', () => {
    const out = collapseDuplicateEchoes([
      msg(1, { direction: 'out', source: 'is' }),
      msg(2, { direction: 'out', source: 'rf', sent_at: '2026-07-27T15:06:02Z' }),
    ]);
    assert.equal(out.length, 2);
  });

  it('never merges invite bubbles even if text/sender/timing line up', () => {
    const out = collapseDuplicateEchoes([
      msg(1, { kind: 'invite', text: 'invite' }),
      msg(2, { kind: 'invite', text: 'invite', sent_at: '2026-07-27T15:06:02Z' }),
    ]);
    assert.equal(out.length, 2);
  });

  it('breaks a run when a different message interleaves', () => {
    const out = collapseDuplicateEchoes([
      msg(1, { source: 'is' }),
      msg(2, { from_call: 'OTHER', text: 'hi', sent_at: '2026-07-27T15:06:01Z' }),
      msg(3, { source: 'rf', sent_at: '2026-07-27T15:06:02Z' }),
    ]);
    assert.equal(out.length, 3);
  });

  it('OR-combines unread across a merged group', () => {
    const out = collapseDuplicateEchoes([
      msg(1, { unread: false }),
      msg(2, { unread: true, sent_at: '2026-07-27T15:06:02Z' }),
    ]);
    assert.equal(out.length, 1);
    assert.equal(out[0].unread, true);
  });

  it('dedups repeated identical sources into one badge', () => {
    const out = collapseDuplicateEchoes([
      msg(1, { source: 'rf' }),
      msg(2, { source: 'rf', sent_at: '2026-07-27T15:06:02Z' }),
    ]);
    assert.deepEqual(out[0].mergedSources, ['rf']);
    assert.deepEqual(out[0].mergedIds, [1, 2]);
  });

  it('chains three consecutive echoes into one bubble', () => {
    const out = collapseDuplicateEchoes([
      msg(1, { source: 'is', sent_at: '2026-07-27T15:06:00Z' }),
      msg(2, { source: 'rf', sent_at: '2026-07-27T15:06:02Z' }),
      msg(3, { source: 'rf', sent_at: '2026-07-27T15:06:04Z' }),
    ]);
    assert.equal(out.length, 1);
    assert.deepEqual(out[0].mergedIds, [1, 2, 3]);
    assert.deepEqual(out[0].mergedSources, ['is', 'rf']);
  });

  it('handles an empty/undefined input', () => {
    assert.deepEqual(collapseDuplicateEchoes([]), []);
    assert.deepEqual(collapseDuplicateEchoes(undefined), []);
  });
});
