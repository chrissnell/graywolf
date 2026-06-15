// Live Map radar loop store: a thin $state wrapper around radar-frames-core.js
// that owns the manifest poll and the playback timer. The pure loop math is in
// radar-frames-core.js (unit-tested); this file is the runtime glue.
//
// `load` is injected (LiveMapV2 supplies a fn that reads the bearer token and
// fetches/parses /radar/manifest.json) so the store has no network/token
// knowledge of its own.

import { applyManifest, shouldApply, nextIndex, newestIndex, clampIndex } from './radar-frames-core.js';

export function createRadarFrames({
  load,
  pollMs = 15000, // manifest refresh cadence (matches the worker's manifest TTL)
  frameMs = 160, // ~6 fps playback
  holdMs = 600, // dwell on the newest frame before looping
}) {
  let frames = $state([]); // oldest -> newest
  let index = $state(0);
  let playing = $state(false);
  let pollTimer = null;
  let frameTimer = null;

  function scheduleFrame() {
    if (frameTimer) clearTimeout(frameTimer);
    frameTimer = null;
    if (!playing || frames.length <= 1) return;
    const atNewest = index >= frames.length - 1;
    frameTimer = setTimeout(() => {
      index = nextIndex(index, frames.length);
      scheduleFrame();
    }, atNewest ? holdMs : frameMs);
  }

  function play() {
    if (frames.length <= 1) return;
    playing = true;
    scheduleFrame();
  }
  function pause() {
    playing = false;
    if (frameTimer) clearTimeout(frameTimer);
    frameTimer = null;
  }
  function toggle() {
    if (playing) pause();
    else play();
  }
  // Stop: halt playback and jump back to the latest (newest) frame -- "show me
  // current weather".
  function stop() {
    pause();
    index = newestIndex(frames.length);
  }
  // Scrub: dragging the slider pauses and seeks.
  function seek(i) {
    pause();
    index = clampIndex(i, frames.length);
  }

  async function poll() {
    let list;
    try {
      list = await load();
    } catch {
      return; // keep last-known frames on a thrown failure
    }
    // The injected loader swallows transient failures (401/503/parse) into an
    // empty list rather than throwing; don't let that wipe an established loop.
    if (!shouldApply(frames.length, list)) return;
    const res = applyManifest({ frames, index }, list);
    frames = res.frames;
    index = res.index;
    if (playing) scheduleFrame(); // frame count may have changed
  }

  function startPolling() {
    poll();
    if (pollTimer) clearInterval(pollTimer);
    pollTimer = setInterval(poll, pollMs);
  }
  function stopPolling() {
    if (pollTimer) clearInterval(pollTimer);
    pollTimer = null;
  }
  function destroy() {
    stopPolling();
    pause();
  }

  return {
    get frames() {
      return frames;
    },
    get index() {
      return index;
    },
    get playing() {
      return playing;
    },
    get count() {
      return frames.length;
    },
    get current() {
      return frames[index] ?? null;
    },
    get currentTs() {
      return frames[index]?.ts ?? null;
    },
    play,
    pause,
    toggle,
    stop,
    seek,
    startPolling,
    stopPolling,
    destroy,
  };
}
