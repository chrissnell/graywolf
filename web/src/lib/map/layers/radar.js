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
// Mirrors the other layer modules (stations.js, trails.js): mount returns
// control methods; LiveMapV2 persists settings and drives them via effects,
// and calls refresh() on every data tick. refresh() re-adds the source/layers
// behind existence guards so the overlay survives a basemap setStyle() (which
// rebuilds the style and can drop user-added layers) the same way the sibling
// layers do.

import { radarProviderForRegion, frameBucket, RADAR_REGION_US } from '../sources/radar-source.js';

export function mountRadarLayer(map, { visible, opacity, region = RADAR_REGION_US, frameTs = null, now = () => Date.now() }) {
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

  // Idempotent add: safe to call repeatedly (initial mount + every refresh).
  function ensure() {
    // A per-frame provider has no tile template until a frame ts is known.
    // Add nothing until then -- the overlay is simply absent (mirrors the
    // Worker's pre-manifest 503), and setFrameTs() adds it once a frame loads.
    if (provider.perFrame && curFrameTs == null) return;
    if (!map.getSource(provider.sourceId)) {
      let source;
      if (provider.perFrame) {
        source = { ...provider.source, tiles: provider.frameTiles(curFrameTs) };
      } else if (provider.cacheBust) {
        source = { ...provider.source, tiles: provider.cacheBust(curBucket) };
      } else {
        source = provider.source;
      }
      map.addSource(provider.sourceId, source);
    }
    // Recompute beforeId from the current style -- symbol-layer ids differ
    // across basemaps, so a stale id captured at mount could throw here.
    const firstSymbolId = map.getStyle().layers.find((l) => l.type === 'symbol')?.id;
    for (const layer of provider.layers) {
      if (map.getLayer(layer.id)) continue;
      const spec = {
        ...layer,
        layout: { ...(layer.layout ?? {}), visibility: curVisible ? 'visible' : 'none' },
        paint: { ...(layer.paint ?? {}), [provider.opacity.property]: curOpacity },
      };
      map.addLayer(spec, firstSymbolId);
    }
  }

  ensure();

  function refresh() {
    ensure();
    // RainViewer raster publishes in place at a latest-frame URL; bust
    // MapLibre's in-memory tile cache when the cadence bucket rolls over so the
    // overlay picks up a freshly published frame. The per-frame vector loop
    // doesn't use this -- its frames advance via setFrameTs().
    if (provider.cacheBust) {
      const v = frameBucket(now());
      if (v !== curBucket) {
        curBucket = v;
        const src = map.getSource(provider.sourceId);
        if (src && src.setTiles) src.setTiles(provider.cacheBust(v));
      }
    }
  }

  // Per-frame loop: point the source at frame `ts`. Adds the source on the
  // first ts (when ensure() had nothing to add yet), then swaps the tile
  // template on subsequent frames. No-op for non-perFrame providers (world
  // raster) or a repeated ts.
  function setFrameTs(ts) {
    if (!provider.perFrame || ts == null || ts === curFrameTs) return;
    curFrameTs = ts;
    if (!map.getSource(provider.sourceId)) {
      ensure();
      return;
    }
    const src = map.getSource(provider.sourceId);
    if (src && src.setTiles) src.setTiles(provider.frameTiles(ts));
  }

  function setVisible(v) {
    curVisible = v;
    const value = v ? 'visible' : 'none';
    for (const layer of provider.layers) {
      if (map.getLayer(layer.id)) {
        map.setLayoutProperty(layer.id, 'visibility', value);
      }
    }
  }

  function setOpacity(v) {
    curOpacity = v;
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
  function setRegion(region) {
    if (region === curRegion) return;
    curRegion = region;
    destroy();
    provider = radarProviderForRegion(region);
    curBucket = provider.cacheBust ? frameBucket(now()) : null;
    ensure();
  }

  function destroy() {
    try {
      for (const layer of provider.layers) {
        if (map.getLayer(layer.id)) map.removeLayer(layer.id);
      }
      if (map.getSource(provider.sourceId)) map.removeSource(provider.sourceId);
    } catch { /* map already removed */ }
  }

  return { refresh, setVisible, setOpacity, setRegion, setFrameTs, destroy };
}
