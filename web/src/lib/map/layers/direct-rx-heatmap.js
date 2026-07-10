// MapLibre heatmap layer for directly-received RF packets. Follows the
// radar.js pattern: ensure() re-adds the source+layer after a basemap
// setStyle() drops user layers; refresh() feeds new data.
import { normalizeFeatureWeights } from '../sources/heatmap-source.js';

const SRC = 'direct-rx-heatmap-src';
const LYR = 'direct-rx-heatmap';

const EMPTY = { type: 'FeatureCollection', features: [] };

// Insert the heatmap below the first trail/line layer if present so trails
// stay readable; DOM station markers always render above the GL canvas.
function beforeId(map) {
  for (const id of ['gw-trails-line', 'gw-trails-dot', 'gw-trails-dot-hit']) {
    if (map.getLayer(id)) return id;
  }
  return undefined;
}

export function mountHeatmapLayer(map, { visible = false, opacity = 0.8 } = {}) {
  let lastData = EMPTY;
  let lastMax = 0;
  let curOpacity = opacity;

  function ensure() {
    if (!map.getSource(SRC)) {
      map.addSource(SRC, { type: 'geojson', data: lastData });
    }
    if (!map.getLayer(LYR)) {
      map.addLayer(
        {
          id: LYR,
          type: 'heatmap',
          source: SRC,
          paint: {
            'heatmap-weight': ['coalesce', ['get', 'w'], 0],
            'heatmap-intensity': ['interpolate', ['linear'], ['zoom'], 0, 1, 12, 3],
            'heatmap-radius': ['interpolate', ['linear'], ['zoom'], 0, 8, 12, 24],
            'heatmap-opacity': curOpacity,
            'heatmap-color': [
              'interpolate', ['linear'], ['heatmap-density'],
              0, 'rgba(0,0,255,0)',
              0.2, 'rgba(0,128,255,0.6)',
              0.4, 'rgba(0,255,128,0.7)',
              0.6, 'rgba(255,255,0,0.8)',
              0.8, 'rgba(255,128,0,0.9)',
              1, 'rgba(255,0,0,1)',
            ],
          },
        },
        beforeId(map),
      );
      map.setLayoutProperty(LYR, 'visibility', visible ? 'visible' : 'none');
    }
  }

  function refresh(geojson, maxCount) {
    if (geojson) {
      lastData = { type: 'FeatureCollection', features: normalizeFeatureWeights(geojson.features, maxCount) };
      lastMax = maxCount ?? 0;
    }
    ensure();
    const src = map.getSource(SRC);
    if (src) src.setData(lastData);
  }

  function setVisible(v) {
    visible = v;
    ensure();
    map.setLayoutProperty(LYR, 'visibility', v ? 'visible' : 'none');
  }

  function setOpacity(v) {
    curOpacity = v;
    ensure();
    map.setPaintProperty(LYR, 'heatmap-opacity', v);
  }

  function destroy() {
    // Guard: on the context-loss remount path (graywolf#461) this runs
    // against a map whose remove() already nulled map.style, so getLayer()
    // would throw. Match the other GL layers (trails, radar, hover-path) so a
    // stale-map teardown can't strand the rest of teardownMapGeneration().
    try {
      if (map.getLayer(LYR)) map.removeLayer(LYR);
      if (map.getSource(SRC)) map.removeSource(SRC);
    } catch { /* map already removed */ }
  }

  ensure();
  return {
    refresh,
    setVisible,
    setOpacity,
    ensure,
    destroy,
    get maxCount() {
      return lastMax;
    },
  };
}
