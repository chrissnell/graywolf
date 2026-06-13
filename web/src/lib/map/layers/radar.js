// NEXRAD radar overlay layer for the Live Map.
//
// Backend-agnostic: it asks radar-source.js for a provider descriptor and
// performs the MapLibre source/layer calls. Raster today, GRA-48 vector
// tomorrow -- this file does not change when the backend flips; only
// ACTIVE_RADAR_BACKEND in radar-source.js does.
//
// Mirrors the other layer modules (stations.js, trails.js): mount returns
// control methods; LiveMapV2 persists settings and drives them via effects,
// and calls refresh() on every data tick. refresh() re-adds the source/layers
// behind existence guards so the overlay survives a basemap setStyle() (which
// rebuilds the style and can drop user-added layers) the same way the sibling
// layers do.

import { radarProvider, frameBucket } from '../sources/radar-source.js';

export function mountRadarLayer(map, { visible, opacity, now = () => Date.now() }) {
  const provider = radarProvider();
  // Last-known UI state, applied when (re-)adding layers after a style swap.
  let curVisible = visible;
  let curOpacity = opacity;
  // Current frame cache-bust bucket (vector backend only). The source is added
  // already pointing at this bucket's URL; refresh() bumps it on rollover.
  let curBucket = provider.cacheBust ? frameBucket(now()) : null;

  // Idempotent add: safe to call repeatedly (initial mount + every refresh).
  function ensure() {
    if (!map.getSource(provider.sourceId)) {
      const source = provider.cacheBust
        ? { ...provider.source, tiles: provider.cacheBust(curBucket) }
        : provider.source;
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
    // Vector frames publish in place at a cycle-less URL; bust MapLibre's
    // in-memory tile cache when the cadence bucket rolls over so the overlay
    // picks up a freshly published frame.
    if (provider.cacheBust) {
      const v = frameBucket(now());
      if (v !== curBucket) {
        curBucket = v;
        const src = map.getSource(provider.sourceId);
        if (src && src.setTiles) src.setTiles(provider.cacheBust(v));
      }
    }
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

  function destroy() {
    for (const layer of provider.layers) {
      if (map.getLayer(layer.id)) map.removeLayer(layer.id);
    }
    if (map.getSource(provider.sourceId)) map.removeSource(provider.sourceId);
  }

  return { refresh, setVisible, setOpacity, destroy };
}
