// NEXRAD radar overlay layer for the Live Map.
//
// Backend-agnostic: it asks radar-source.js for a provider descriptor and
// performs the MapLibre source/layer calls. The active backend is the
// vector contour loop, selected by ACTIVE_RADAR_BACKEND in radar-source.js.
//
// Per-frame vector loop (the smooth-loop design): each manifest frame is its
// OWN source + fill layer, keyed by its immutable epoch ts. setFrames() mounts
// every known frame up front at fill-opacity 0 so its tiles load once and stay
// cached; setFrameTs() then animates the loop by handing the visible opacity
// from the old frame to the new one. That is a pure paint-property toggle --
// no setTiles, no source reload -- so looping reuses the already-loaded frames
// instead of re-fetching tiles on every cycle (the old single-source + setTiles
// design refetched and re-parsed every frame each loop, which made the
// animation choppy).
//
// The RainViewer world raster backend is ALSO a per-frame loop now: the origin
// Worker exposes /radar/rainviewer/manifest.json + immutable per-frame tiles
// (/radar/rainviewer/{ts}/{z}/{x}/{y}.png), so the world overlay rides the exact
// same per-frame source/opacity machinery as the US vector loop -- only the
// source type (raster) and tile template differ. The single-source ensure()
// path below now serves only the inactive US IEM raster fallback backend
// (ACTIVE_RADAR_BACKEND), a plain static-tiles raster.
//
// Mirrors the other layer modules (stations.js, trails.js): mount returns
// control methods; LiveMapV2 persists settings and drives them via effects,
// and calls refresh() on every data tick. refresh() re-adds the source/layers
// behind existence guards so the overlay survives a basemap setStyle() (which
// rebuilds the style and can drop user-added layers) the same way the sibling
// layers do.

import { radarProviderForRegion, RADAR_REGION_US } from '../sources/radar-source.js';

export function mountRadarLayer(
  map,
  { visible, opacity, region = RADAR_REGION_US, frameTs = null, frames = null },
) {
  // Region (US vs rest-of-world) is operator-selectable, so the provider is
  // mutable: setRegion() tears down and rebuilds it. Everything below reads the
  // current `provider`, so the same add/remove logic serves either region.
  let curRegion = region;
  let provider = radarProviderForRegion(curRegion);
  // Last-known UI state, applied when (re-)adding layers after a style swap or
  // a region switch.
  let curVisible = visible;
  let curOpacity = opacity;
  // Current frame ts (per-frame loop only): the frame currently painted
  // at full opacity. Seeded from the mount option when the manifest poll already
  // resolved before the layer mounted; null otherwise.
  let curFrameTs = frameTs;
  // The set of frame ts that should be mounted (one source+layer each). Seeded
  // from the mount options; setFrames() reconciles it against the manifest as
  // frames roll in and out, and ensure() re-adds anything MapLibre dropped on a
  // style swap.
  const mounted = new Set(Array.isArray(frames) ? frames : []);
  if (curFrameTs != null) mounted.add(curFrameTs);

  // Per-frame source/layer ids derive from the provider's base ids so the world
  // raster path (single source) and the vector loop (one per frame) never collide.
  const frameSourceId = (ts) => `${provider.sourceId}-${ts}`;
  const frameLayerId = (ts) => `${provider.layers[0].id}-${ts}`;
  const opacityProp = () => provider.opacity.property;
  const firstSymbolId = () => map.getStyle().layers.find((l) => l.type === 'symbol')?.id;

  // Per-frame: add the source + fill layer for one frame ts if absent. The
  // current frame paints at curOpacity; every other frame is mounted at opacity
  // 0 so its tiles preload and cache without being seen -- advancing the loop is
  // then a pure paint-property toggle.
  function ensureFrame(ts) {
    mounted.add(ts);
    if (!map.getSource(frameSourceId(ts))) {
      map.addSource(frameSourceId(ts), { ...provider.source, tiles: provider.frameTiles(ts) });
    }
    if (map.getLayer(frameLayerId(ts))) return;
    const layer = provider.layers[0];
    const spec = {
      ...layer,
      id: frameLayerId(ts),
      source: frameSourceId(ts),
      layout: { ...(layer.layout ?? {}), visibility: curVisible ? 'visible' : 'none' },
      paint: { ...(layer.paint ?? {}), [opacityProp()]: ts === curFrameTs ? curOpacity : 0 },
    };
    map.addLayer(spec, firstSymbolId());
  }

  function removeFrame(ts) {
    if (map.getLayer(frameLayerId(ts))) map.removeLayer(frameLayerId(ts));
    if (map.getSource(frameSourceId(ts))) map.removeSource(frameSourceId(ts));
    mounted.delete(ts);
  }

  // Idempotent add: safe to call repeatedly (initial mount + every refresh).
  function ensure() {
    if (provider.perFrame) {
      // Per-frame loop has nothing to add until a frame ts is known; the overlay
      // is simply absent (mirrors the Worker's pre-manifest 503). Re-add every
      // frame that should be mounted (recovers from a style swap).
      for (const ts of mounted) ensureFrame(ts);
      return;
    }
    // Single-source backend (the inactive US IEM raster fallback): one static
    // raster source + layer.
    if (!map.getSource(provider.sourceId)) {
      map.addSource(provider.sourceId, provider.source);
    }
    // Recompute beforeId from the current style -- symbol-layer ids differ
    // across basemaps, so a stale id captured at mount could throw here.
    const beforeId = firstSymbolId();
    for (const layer of provider.layers) {
      if (map.getLayer(layer.id)) continue;
      const spec = {
        ...layer,
        layout: { ...(layer.layout ?? {}), visibility: curVisible ? 'visible' : 'none' },
        paint: { ...(layer.paint ?? {}), [provider.opacity.property]: curOpacity },
      };
      map.addLayer(spec, beforeId);
    }
  }

  ensure();

  // Re-add the source/layers behind existence guards so the overlay survives a
  // basemap setStyle() (which rebuilds the style and can drop user-added
  // layers). Frames advance via setFrameTs(), not here.
  function refresh() {
    ensure();
  }

  // Per-frame loop: reconcile the mounted frames against the manifest's frame
  // list (oldest->newest). New frames are preloaded at opacity 0; frames that
  // have rolled off the manifest are torn down (never the visible one). No-op
  // for single-source backends (world raster).
  //
  // The mounted set is bounded by the manifest window -- it is exactly the
  // frames the loop plays (the slider and Play/Pause iterate the same list), so
  // capping it here would desync the overlay from the loop store. The manifest's
  // length (the deployed generator's loop span, ~3h) is therefore the bound on
  // both the source count and the one-time preload fetch burst.
  function setFrames(list) {
    if (!provider.perFrame) return;
    const next = new Set(Array.isArray(list) ? list : []);
    for (const ts of [...mounted]) {
      if (!next.has(ts) && ts !== curFrameTs) removeFrame(ts);
    }
    for (const ts of next) ensureFrame(ts);
  }

  // Per-frame loop: make frame `ts` the visible one. Hands the visible opacity
  // from the previous frame to this one -- no setTiles, no source reload, since
  // every frame's tiles are already cached. Mounts the frame on demand if the
  // manifest race beat setFrames(). No-op for single-source backends or a
  // repeated ts.
  function setFrameTs(ts) {
    if (!provider.perFrame || ts == null || ts === curFrameTs) return;
    const prev = curFrameTs;
    curFrameTs = ts;
    ensureFrame(ts);
    if (prev != null && map.getLayer(frameLayerId(prev))) {
      map.setPaintProperty(frameLayerId(prev), opacityProp(), 0);
    }
    if (map.getLayer(frameLayerId(ts))) {
      map.setPaintProperty(frameLayerId(ts), opacityProp(), curOpacity);
    }
  }

  // Toggle the overlay. For the per-frame loop this hides every frame layer via
  // layout visibility (not opacity): an all-`none` source is unused, so MapLibre
  // is free to evict its tiles while radar is off -- the first loop after a
  // re-enable refetches, which is intended (better than keeping dozens of hidden
  // sources warming the network) and not a regression of the no-refetch loop.
  function setVisible(v) {
    curVisible = v;
    const value = v ? 'visible' : 'none';
    if (provider.perFrame) {
      for (const ts of mounted) {
        if (map.getLayer(frameLayerId(ts))) map.setLayoutProperty(frameLayerId(ts), 'visibility', value);
      }
      return;
    }
    for (const layer of provider.layers) {
      if (map.getLayer(layer.id)) {
        map.setLayoutProperty(layer.id, 'visibility', value);
      }
    }
  }

  function setOpacity(v) {
    curOpacity = v;
    if (provider.perFrame) {
      // Only the visible frame carries opacity; every other frame stays at 0.
      if (curFrameTs != null && map.getLayer(frameLayerId(curFrameTs))) {
        map.setPaintProperty(frameLayerId(curFrameTs), opacityProp(), v);
      }
      return;
    }
    for (const id of provider.opacity.layerIds) {
      if (map.getLayer(id)) {
        map.setPaintProperty(id, provider.opacity.property, v);
      }
    }
  }

  // Switch coverage region. The US and world providers can differ in layer
  // type/ids (vector fill vs raster), so we fully tear down the current
  // provider's layers + source and rebuild from the new one. curVisible /
  // curOpacity carry over, so ensure() re-applies the operator's UI state.
  // Frame ts are region-specific -- US contour ts and RainViewer ts are
  // different namespaces, and a frame's tiles belong to one provider -- so the
  // loop is cleared on a switch; the store re-polls the new region's manifest
  // and re-drives setFrames()/setFrameTs() (LiveMapV2 resets the loop in step).
  function setRegion(region) {
    if (region === curRegion) return;
    curRegion = region;
    destroy();
    provider = radarProviderForRegion(region);
    mounted.clear();
    curFrameTs = null;
    ensure();
  }

  function destroy() {
    try {
      if (provider.perFrame) {
        for (const ts of [...mounted]) removeFrame(ts);
        return;
      }
      for (const layer of provider.layers) {
        if (map.getLayer(layer.id)) map.removeLayer(layer.id);
      }
      if (map.getSource(provider.sourceId)) map.removeSource(provider.sourceId);
    } catch { /* map already removed */ }
  }

  return { refresh, setFrames, setVisible, setOpacity, setRegion, setFrameTs, destroy };
}
