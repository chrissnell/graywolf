// Radar layer: a single vector source of smoothed dBZ isobands, painted as
// recolorable fill. Polls latest.json and swaps the source tile template when
// a new frame appears (each frame's URL is unique, so MapLibre's tile cache
// never serves a stale frame). Mirrors the stations/trails/weather layer
// contract: returns { refresh, destroy, setVisible, setOpacity, ... }.
import {
  RADAR_SOURCE_ID, RADAR_LAYER_ID, RADAR_LATEST_URL,
  fillColorExpression, radarTileUrl,
} from '../sources/radar-source.js';

const DEFAULT_OPACITY = 0.7;

export function mountRadarLayer(map, opts = {}) {
  const fetchLatest = opts.fetchLatest ?? (async () => {
    const res = await fetch(RADAR_LATEST_URL, { cache: 'no-cache' });
    if (!res.ok) throw new Error(`radar latest: ${res.status}`);
    return res.json();
  });
  const minzoom = opts.minzoom ?? 3;
  const maxzoom = opts.maxzoom ?? 10;

  let currentTs = null, opacity = DEFAULT_OPACITY, visible = true;

  function ensureLayer(ts) {
    if (!map.getSource(RADAR_SOURCE_ID)) {
      map.addSource(RADAR_SOURCE_ID, { type: 'vector', tiles: [radarTileUrl(ts)], minzoom, maxzoom });
    }
    if (!map.getLayer(RADAR_LAYER_ID)) {
      map.addLayer({
        id: RADAR_LAYER_ID, type: 'fill', source: RADAR_SOURCE_ID, 'source-layer': 'radar',
        paint: { 'fill-color': fillColorExpression(), 'fill-opacity': opacity, 'fill-antialias': true },
        layout: { visibility: visible ? 'visible' : 'none' },
      });
    }
  }

  async function refresh() {
    let latest;
    try { latest = await fetchLatest(); } catch { return; }
    if (!latest || !latest.ts) return;
    ensureLayer(latest.ts);
    if (latest.ts !== currentTs) {
      currentTs = latest.ts;
      const src = map.getSource(RADAR_SOURCE_ID);
      if (src && src.setTiles) src.setTiles([radarTileUrl(latest.ts)]);
    }
  }

  function setOpacity(next) {
    opacity = next;
    if (map.getLayer(RADAR_LAYER_ID)) map.setPaintProperty(RADAR_LAYER_ID, 'fill-opacity', opacity);
  }
  function setVisible(next) {
    visible = !!next;
    if (map.getLayer(RADAR_LAYER_ID)) map.setLayoutProperty(RADAR_LAYER_ID, 'visibility', visible ? 'visible' : 'none');
  }

  let timer = null;
  function startPolling(intervalMs = 300000) { stopPolling(); timer = setInterval(() => { refresh(); }, intervalMs); }
  function stopPolling() { if (timer) { clearInterval(timer); timer = null; } }
  function destroy() {
    stopPolling();
    if (map.getLayer(RADAR_LAYER_ID)) map.removeLayer(RADAR_LAYER_ID);
    if (map.getSource(RADAR_SOURCE_ID)) map.removeSource(RADAR_SOURCE_ID);
  }

  return { refresh, destroy, setVisible, setOpacity, startPolling, stopPolling };
}
