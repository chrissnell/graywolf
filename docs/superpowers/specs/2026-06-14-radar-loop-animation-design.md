# Live Map radar loop animation — design

**Status:** approved (issue GRA-78 thread, 2026-06-14)

## Goal

The graywolf-maps origin Worker now serves the US radar contours as a rolling
~3-hour loop: a manifest (`GET /radar/manifest.json`) lists frames, and each
frame is an immutable per-frame tile URL `GET /radar/{ts}/{z}/{x}/{y}.pbf`
(`ts` = 10-digit Unix epoch). The legacy cycle-less `/radar/{z}/{x}/{y}.pbf`
route is removed.

Two things follow:

1. **Required migration.** The Live Map radar overlay currently fetches the
   retired cycle-less URL with a `?v=` cache-bust. When the Worker change ships,
   that route 404s and the `?v=` param is rejected (400 on the contour path), so
   the US overlay goes dark unless the client moves to the manifest scheme.
2. **Requested feature.** Operators want to animate the loop: `[Play][Stop]`
   square buttons in the layers pane and a slider showing the current frame.

This is one change: migrate to manifest-driven per-frame tiles, then add the
playback controls on top.

Scope: US vector contour overlay only. The RainViewer rest-of-world raster
overlay is unchanged (it keeps its `?v=` cache-bust; the Worker exempts the
raster path from the query rejection).

## Frame transition (decision: A, with B as a fast-follow)

**A — tile-swap per frame (this change):** one vector source; advancing a frame
calls `source.setTiles([url(ts)])`. Per-frame tiles are immutable and
edge-cached for a year, so after the first loop pass every frame is served from
the CF edge and swaps are instant. Lowest risk; mirrors the existing cache-bust
seam.

**B — two-layer opacity crossfade (future):** ping-pong two fill layers and
crossfade opacity for buttery transitions. More code; deferred.

## Playback defaults (approved)

- ~2 fps (500 ms/frame), ~1 s hold on the newest frame, then loop to oldest.
- Starts **paused on the latest frame**; the operator presses Play.
- Dragging the slider pauses playback and scrubs.

## Components

### `radar-frames.svelte.js` (new, pure/testable)

`createRadarFrames({ load, now, pollMs, frameMs, holdMs })` returns a store:

- State: `frames` (oldest→newest `{ ts, iso }`), `index` (into `frames`),
  `playing`.
- Derived getters: `current`, `currentTs`, `count`.
- `applyManifest(list)` — replace frames, preserving continuity: if parked on
  the newest frame, stay newest; else keep the same `ts` if still present, else
  clamp. Keeps `playing` as-is.
- `advance()` — one playback step: `index++`, wrapping to 0 after the newest
  frame. Exposed so tests drive stepping deterministically; the internal timer
  calls it on a `frameMs`/`holdMs` schedule.
- `play()`, `pause()`, `toggle()`, `stop()` (pause + jump to newest),
  `seek(i)` (pause + clamp).
- `start()` / `destroy()` — wire/tear down the manifest poll (`pollMs`) and the
  playback timer. `load` is injected (LiveMapV2 supplies a fn that reads the
  bearer token and fetches/parses the manifest) so the store is unit-testable
  with no network or token.

### `radar-source.js` (edit)

- `vectorTileUrl(ts)` → `${BASE}/radar/${ts}/{z}/{x}/{y}.pbf` (ts required; no
  `?v=`).
- `radarManifestUrl()` → `${BASE}/radar/manifest.json`.
- `parseManifestFrames(json)` → oldest→newest `{ ts, iso }[]` from a
  `schema_version: 1` manifest (the manifest is newest-first; reverse). `[]` on
  bad input.
- Vector provider: replace `cacheBust` with `perFrame: true` +
  `frameTiles: (ts) => [vectorTileUrl(ts)]`.
- `frameBucket` / `rainviewerTileUrl` unchanged (RainViewer raster still
  cache-busts).

### `radar.js` (edit)

- Track `curFrameTs`. For a `perFrame` provider, `ensure()` adds the source only
  once a ts is known (`tiles: provider.frameTiles(curFrameTs)`); before that the
  overlay is simply absent (matches the Worker's pre-manifest 503).
- `setFrameTs(ts)` — set the current frame and `setTiles` (or add the source if
  not yet present). No-op for raster providers.
- `refresh()` — perFrame: just re-`ensure()` (survives a basemap `setStyle`).
  Raster cache-bust path unchanged.
- `setRegion` carries `curFrameTs` across a US↔world switch.

### `LiveMapV2.svelte` (edit)

- Build `load` (reads `mapsState.revealToken()`, fetches `radarManifestUrl()`
  with `?t=`, returns `parseManifestFrames`). Create the frames store; `start()`
  on map ready, `destroy()` on teardown; only poll while radar is visible.
- Effect: push `radarFrames.currentTs` into `radarLayer.setFrameTs(...)`.
- UI in the shared `panelBody` snippet (desktop card + mobile drawer), shown
  when radar is visible: a row of two **square** buttons `[Play/Pause][Stop]`
  (lucide `Play`/`Pause`/`Square`), and a frame `<input type=range>` bound to
  the frame index with a label showing the frame's local time + position
  (e.g. `6:05 PM · 34/37`). Dragging seeks (pauses).

## Testing (Node `--test`, matching the repo)

- `radar-frames`: `applyManifest` continuity (newest-parked vs same-ts vs
  clamp), `advance` wrap, `stop`/`seek`/`play`/`pause` transitions, load-error
  keeps last-known frames.
- `radar-source`: `vectorTileUrl(ts)` shape, `parseManifestFrames` ordering +
  bad-input, provider `perFrame`/`frameTiles`.
- `radar.js`: `setFrameTs` adds the source on first ts and swaps tiles after;
  `perFrame` source not added before a ts; region switch carries the frame.

## Cross-repo coupling

- graywolf-maps PR #20 must ship with (or before) this client change.
- The Worker fix exempting the raster path from the query rejection is part of
  PR #20.
