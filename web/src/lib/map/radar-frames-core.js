// Pure loop state machine for the Live Map radar animation. No runes, no DOM,
// no network -- so it is unit-testable under `node --test`. radar-frames.svelte.js
// wraps this with $state and the poll/animation timers (mirrors the
// releaseNotesCore.js / releaseNotesStore.svelte.js split).
//
// A frame is { ts:number, iso:string }. Lists are ALWAYS oldest -> newest, so a
// rising index advances forward in time and the slider reads left(old)->right(new).

// Clamp a frame index into [0, count-1] (0 when there are no frames).
export function clampIndex(i, count) {
  if (count <= 0) return 0;
  if (i < 0) return 0;
  if (i >= count) return count - 1;
  return i;
}

// The newest frame's index (the "latest", rightmost). 0 when empty.
export function newestIndex(count) {
  return count > 0 ? count - 1 : 0;
}

// One playback step: advance a frame, wrapping back to the oldest after the
// newest so the loop repeats.
export function nextIndex(i, count) {
  if (count <= 0) return 0;
  return i >= count - 1 ? 0 : i + 1;
}

// Whether a freshly-loaded list should replace the current frames. A transient
// load failure surfaces as an empty/absent list (the injected loader swallows
// 401/503/parse errors into []), so an established loop must NOT be wiped over a
// blip -- keep the last-known frames and retry next poll. An empty list is only
// accepted while we have nothing yet (bootstrap).
export function shouldApply(prevCount, list) {
  if (Array.isArray(list) && list.length > 0) return true;
  return prevCount === 0;
}

// Merge a freshly-fetched frame list (oldest->newest) while preserving playback
// continuity. Returns { frames, index }:
//   - bootstrapping (no prior frames) or parked on the newest frame -> stay
//     newest, so a newly published frame becomes the shown frame.
//   - otherwise keep the same ts if it still exists (the operator is scrubbing
//     or paused mid-loop), else clamp the old index into the new range.
export function applyManifest(prev, list) {
  const frames = Array.isArray(list) ? list : [];
  if (frames.length === 0) return { frames, index: 0 };
  const prevFrames = (prev && prev.frames) || [];
  const prevCount = prevFrames.length;
  const prevIndex = (prev && prev.index) || 0;
  if (prevCount === 0 || prevIndex >= prevCount - 1) {
    return { frames, index: newestIndex(frames.length) };
  }
  const curTs = prevFrames[prevIndex] && prevFrames[prevIndex].ts;
  const found = frames.findIndex((f) => f.ts === curTs);
  return { frames, index: found >= 0 ? found : clampIndex(prevIndex, frames.length) };
}
