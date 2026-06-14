import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  clampIndex,
  newestIndex,
  nextIndex,
  applyManifest,
  shouldApply,
} from './radar-frames-core.js';

const F = (ts) => ({ ts, iso: `iso-${ts}` });

test('clampIndex keeps the index in range', () => {
  assert.equal(clampIndex(-1, 5), 0);
  assert.equal(clampIndex(2, 5), 2);
  assert.equal(clampIndex(9, 5), 4);
  assert.equal(clampIndex(3, 0), 0); // empty
});

test('newestIndex is the last frame (0 when empty)', () => {
  assert.equal(newestIndex(0), 0);
  assert.equal(newestIndex(1), 0);
  assert.equal(newestIndex(37), 36);
});

test('nextIndex advances and wraps oldest->newest->oldest', () => {
  assert.equal(nextIndex(0, 3), 1);
  assert.equal(nextIndex(1, 3), 2);
  assert.equal(nextIndex(2, 3), 0); // wrap after newest
  assert.equal(nextIndex(0, 0), 0); // empty
});

test('applyManifest bootstraps onto the newest frame', () => {
  const r = applyManifest({ frames: [], index: 0 }, [F(1), F(2), F(3)]);
  assert.deepEqual(r.frames.map((f) => f.ts), [1, 2, 3]);
  assert.equal(r.index, 2); // newest
});

test('applyManifest stays parked on newest as new frames arrive', () => {
  // Parked on newest (index 2 of 3); a 4th frame publishes -> follow to newest.
  const r = applyManifest({ frames: [F(1), F(2), F(3)], index: 2 }, [F(1), F(2), F(3), F(4)]);
  assert.equal(r.index, 3);
  assert.equal(r.frames[r.index].ts, 4);
});

test('applyManifest keeps the same ts when scrubbing mid-loop', () => {
  // Paused on ts=2 (index 1). A new frame appends + oldest rolls off.
  const r = applyManifest({ frames: [F(1), F(2), F(3)], index: 1 }, [F(2), F(3), F(4)]);
  assert.equal(r.frames[r.index].ts, 2); // still on ts=2 (now index 0)
  assert.equal(r.index, 0);
});

test('applyManifest clamps when the parked ts has rolled off', () => {
  // Paused on ts=1 (index 0); ts=1 rolls off entirely.
  const r = applyManifest({ frames: [F(1), F(2), F(3)], index: 0 }, [F(2), F(3), F(4)]);
  assert.equal(r.index, 0); // clamped into range
  assert.equal(r.frames[r.index].ts, 2);
});

test('shouldApply keeps an established loop when a load comes back empty', () => {
  // Transient failure (load swallowed an error into []) -- keep last-known.
  assert.equal(shouldApply(3, []), false);
  assert.equal(shouldApply(3, null), false);
  assert.equal(shouldApply(3, undefined), false);
  // Bootstrap: accept an empty result only while we have nothing yet.
  assert.equal(shouldApply(0, []), true);
  // A real list always applies.
  assert.equal(shouldApply(3, [F(1)]), true);
  assert.equal(shouldApply(0, [F(1)]), true);
});

test('applyManifest handles an empty/absent new list', () => {
  assert.deepEqual(applyManifest({ frames: [F(1)], index: 0 }, []), { frames: [], index: 0 });
  assert.deepEqual(applyManifest({ frames: [], index: 0 }, null), { frames: [], index: 0 });
});
