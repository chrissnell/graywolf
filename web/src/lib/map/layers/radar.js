// NEXRAD radar overlay layer for the Live Map.
//
// Backend-agnostic: it asks radar-source.js for a provider descriptor and
// performs the MapLibre source/layer calls. The active backend is the
// vector contour loop, selected by ACTIVE_RADAR_BACKEND in radar-source.js.
// The vector backend is a per-frame loop: each frame is an immutable URL keyed
// by its epoch ts, and the LiveMap animation calls setFrameTs(ts) to swap the
// tile template. The RainViewer world raster backend instead carries a
// cadence-aligned `?v=` cache-bust that refresh() bumps on a time-bucket
// rollover; that path is unchanged.
//
// Cross-fade (double buffer): rather than swapping tiles on one source -- which
// hard-cuts to the new frame the instant its tiles land -- the overlay keeps two
// parallel source+layer sets ('a' / 'b'). One buffer holds the on-screen frame
// at the operator's opacity; the other is idle at 0. Advancing a frame loads it
// into the idle buffer, waits for its tiles to render, then cross-fades: the
// incoming buffer ramps up to opacity while the outgoing buffer ramps down to 0,
// using MapLibre's native paint-property transitions. The result is a smooth
// dissolve between frames instead of a flicker.
//
// Mirrors the other layer modules (stations.js, trails.js): mount returns
// control methods; LiveMapV2 persists settings and drives them via effects,
// and calls refresh() on every data tick. refresh() re-adds the source/layers
// behind existence guards so the overlay survives a basemap setStyle() (which
// rebuilds the style and can drop user-added layers) the same way the sibling
// layers do.

import { radarProviderForRegion, frameBucket, RADAR_REGION_US } from '../sources/radar-source.js';

const BUFFERS = ['a', 'b'];

export function mountRadarLayer(map, {
  visible,
  opacity,
  region = RADAR_REGION_US,
  frameTs = null,
  now = () => Date.now(),
  fadeMs = 250, // cross-fade duration; matches the ~4fps (250ms) frame cadence
  loadTimeoutMs = 600, // fall back to fading even if the tile load never settles
}) {
  // Region (US vs rest-of-world) is operator-selectable, so the provider is
  // mutable: setRegion() tears down and rebuilds it. Everything below reads the
  // current `provider`, so the same add/remove logic serves either region.
  let curRegion = region;
  let provider = radarProviderForRegion(curRegion);
  // Last-known UI state, applied when (re-)adding layers after a style swap or
  // a region switch.
  let curVisible = visible;
  let curOpacity = opacity;
  // Current frame cache-bust bucket (RainViewer raster only). The source is
  // added already pointing at this bucket's URL; refresh() bumps it on rollover.
  let curBucket = provider.cacheBust ? frameBucket(now()) : null;
  // Current frame ts (per-frame vector loop only). Seeded from the mount option
  // when the manifest poll already resolved before the layer mounted (so the
  // overlay paints immediately rather than waiting for the next index change);
  // null otherwise. setFrameTs() advances it.
  let curFrameTs = frameTs;

  // Cross-fade bookkeeping. activeBuf names the buffer currently showing the
  // frame at curOpacity; it flips only once a fade actually STARTS, so frames
  // that arrive faster than tiles can load coalesce onto the latest one instead
  // of fading through stale intermediates. `pending` holds the sourcedata
  // listener + fallback timer while we wait for the idle buffer to render.
  let activeBuf = null; // 'a' | 'b' | null (nothing shown yet)
  let pending = null;

  const srcId = (buf) => `${provider.sourceId}-${buf}`;
  const layerIdFor = (layer, buf) => `${layer.id}-${buf}`;

  // Tile template for the frame currently selected (per-frame ts, raster cache
  // bucket, or the provider's static tiles).
  function currentTiles() {
    if (provider.perFrame) return provider.frameTiles(curFrameTs);
    if (provider.cacheBust) return provider.cacheBust(curBucket);
    return provider.source.tiles;
  }

  // Insert position for a buffer's layers. An existing radar layer (either
  // buffer) anchors new layers to the same stack slot, so both buffers stay
  // adjacent and beneath trails/markers; the very first buffer goes just below
  // the first symbol layer (matching the sibling layer modules).
  function anchorBeforeId() {
    for (const buf of BUFFERS) {
      for (const layer of provider.layers) {
        const id = layerIdFor(layer, buf);
        if (map.getLayer(id)) return id;
      }
    }
    return map.getStyle().layers.find((l) => l.type === 'symbol')?.id;
  }

  // Idempotent add for one buffer's source + layers. Layers start at opacity 0
  // with a transition on the opacity property so subsequent setPaintProperty
  // changes animate -- that transition IS the cross-fade.
  function ensureBuffer(buf, tiles) {
    if (!map.getSource(srcId(buf))) {
      map.addSource(srcId(buf), { ...provider.source, tiles });
    }
    const beforeId = anchorBeforeId();
    const prop = provider.opacity.property;
    for (const layer of provider.layers) {
      const id = layerIdFor(layer, buf);
      if (map.getLayer(id)) continue;
      const spec = {
        ...layer,
        id,
        source: srcId(buf),
        layout: { ...(layer.layout ?? {}), visibility: curVisible ? 'visible' : 'none' },
        paint: {
          ...(layer.paint ?? {}),
          [prop]: 0,
          [`${prop}-transition`]: { duration: fadeMs, delay: 0 },
        },
      };
      map.addLayer(spec, beforeId);
    }
  }

  function setBufferOpacity(buf, value) {
    const prop = provider.opacity.property;
    for (const layer of provider.layers) {
      const id = layerIdFor(layer, buf);
      if (map.getLayer(id)) map.setPaintProperty(id, prop, value);
    }
  }

  function cancelPending() {
    if (!pending) return;
    if (pending.handler && map.off) map.off('sourcedata', pending.handler);
    if (pending.timer != null) clearTimeout(pending.timer);
    pending = null;
  }

  // Load `tiles` into the idle buffer and cross-fade to it once its tiles are on
  // screen. Shared by the per-frame loop (setFrameTs) and the world raster
  // cache-bust rollover (refresh).
  function crossfadeTo(tiles) {
    const idle = activeBuf === 'a' ? 'b' : 'a';
    const existed = !!map.getSource(srcId(idle));
    ensureBuffer(idle, tiles);
    if (existed) {
      const src = map.getSource(srcId(idle));
      if (src && src.setTiles) src.setTiles(tiles);
    }

    cancelPending();

    const start = () => {
      cancelPending();
      setBufferOpacity(idle, curOpacity);
      if (activeBuf && activeBuf !== idle) setBufferOpacity(activeBuf, 0);
      activeBuf = idle;
    };

    // Wait for the idle source's tiles to render before fading, so we never fade
    // up into a blank frame; fall back on a timer so a slow load can't stall the
    // loop. A map without an event interface (unit tests) fades immediately.
    if (typeof map.on === 'function') {
      const handler = (e) => {
        if (e && e.sourceId === srcId(idle) && e.isSourceLoaded) start();
      };
      const timer = setTimeout(start, loadTimeoutMs);
      pending = { handler, timer };
      map.on('sourcedata', handler);
      // Already-cached frames may report loaded before any further event fires.
      if (typeof map.isSourceLoaded === 'function' && map.isSourceLoaded(srcId(idle))) start();
    } else {
      start();
    }
  }

  // (Re)build the active buffer in place at full opacity -- a restore after a
  // style swap dropped our layers, not a frame advance, so no fade. Only the
  // active buffer is restored; the idle buffer rebuilds lazily on the next
  // crossfadeTo() (so the first frame after a basemap change re-adds it fresh).
  function restoreActive() {
    ensureBuffer(activeBuf, currentTiles());
    setBufferOpacity(activeBuf, curOpacity);
  }

  function ensure() {
    // A per-frame provider has no tile template until a frame ts is known. Add
    // nothing until then -- the overlay is simply absent (mirrors the Worker's
    // pre-manifest 503); setFrameTs() fades it in once a frame loads.
    if (provider.perFrame && curFrameTs == null) return;
    if (activeBuf == null) {
      crossfadeTo(currentTiles());
    } else {
      restoreActive();
    }
  }

  ensure();

  function refresh() {
    ensure();
    // RainViewer raster publishes in place at a latest-frame URL; on a cadence
    // rollover, cross-fade to the freshly published frame. The per-frame vector
    // loop doesn't use this -- its frames advance via setFrameTs().
    if (provider.cacheBust) {
      const v = frameBucket(now());
      if (v !== curBucket) {
        curBucket = v;
        crossfadeTo(provider.cacheBust(v));
      }
    }
  }

  // Per-frame loop: cross-fade to frame `ts`. No-op for non-perFrame providers
  // (world raster) or a repeated ts.
  function setFrameTs(ts) {
    if (!provider.perFrame || ts == null || ts === curFrameTs) return;
    curFrameTs = ts;
    crossfadeTo(provider.frameTiles(ts));
  }

  function setVisible(v) {
    curVisible = v;
    const value = v ? 'visible' : 'none';
    for (const buf of BUFFERS) {
      for (const layer of provider.layers) {
        const id = layerIdFor(layer, buf);
        if (map.getLayer(id)) map.setLayoutProperty(id, 'visibility', value);
      }
    }
  }

  function setOpacity(v) {
    curOpacity = v;
    // The visible buffer follows the slider; the idle buffer stays parked at 0.
    if (activeBuf) setBufferOpacity(activeBuf, v);
    setBufferOpacity(activeBuf === 'a' ? 'b' : 'a', 0);
  }

  // Switch coverage region. The US and world providers can differ in layer
  // type/ids (vector fill vs raster), so we fully tear down both buffers and
  // rebuild from the new provider. curVisible / curOpacity carry over, so
  // ensure() re-applies the operator's UI state.
  function setRegion(region) {
    if (region === curRegion) return;
    curRegion = region;
    destroy();
    provider = radarProviderForRegion(region);
    curBucket = provider.cacheBust ? frameBucket(now()) : null;
    activeBuf = null;
    ensure();
  }

  function destroy() {
    cancelPending();
    try {
      for (const buf of BUFFERS) {
        for (const layer of provider.layers) {
          const id = layerIdFor(layer, buf);
          if (map.getLayer(id)) map.removeLayer(id);
        }
        if (map.getSource(srcId(buf))) map.removeSource(srcId(buf));
      }
    } catch { /* map already removed */ }
  }

  return { refresh, setVisible, setOpacity, setRegion, setFrameTs, destroy };
}
