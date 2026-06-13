// NEXRAD radar overlay layer for the Live Map.
//
// Backend-agnostic: it asks radar-source.js for a provider descriptor and
// performs the MapLibre source/layer calls. The active backend is the
// GRA-48 vector contours, selected by ACTIVE_RADAR_BACKEND in
// radar-source.js. For the vector backend this layer also drives a
// cadence-aligned frame cache-bust (the route always serves the latest
// frame, so refresh() bumps the tile URL on a time-bucket rollover to make
// MapLibre refetch); the raster backend has no cache-bust and refresh()
// is purely idempotent re-adds.
//
// Mirrors the other layer modules (stations.js, trails.js): mount returns
// control methods; LiveMapV2 persists settings and drives them via effects,
// and calls refresh() on every data tick. refresh() re-adds the source/layers
// behind existence guards so the overlay survives a basemap setStyle() (which
// rebuilds the style and can drop user-added layers) the same way the sibling
// layers do.

import { radarProviderForRegion, frameBucket, RADAR_REGION_US } from '../sources/radar-source.js';

export function mountRadarLayer(map, { visible, opacity, region = RADAR_REGION_US, now = () => Date.now() }) {
  // Region (US vs rest-of-world) is operator-selectable, so the provider is
  // mutable: setRegion() tears down and rebuilds it. Everything below reads the
  // current `provider`, so the same add/remove logic serves either region.
  let curRegion = region;
  let provider = radarProviderForRegion(curRegion);
  // Last-known UI state, applied when (re-)adding layers after a style swap or
  // a region switch.
  let curVisible = visible;
  let curOpacity = opacity;
  // Current frame cache-bust bucket (providers with cacheBust only). The source
  // is added already pointing at this bucket's URL; refresh() bumps it on
  // rollover.
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
    for (const layer of provider.layers) {
      if (map.getLayer(layer.id)) map.removeLayer(layer.id);
    }
    if (map.getSource(provider.sourceId)) map.removeSource(provider.sourceId);
  }

  return { refresh, setVisible, setOpacity, setRegion, destroy };
}
