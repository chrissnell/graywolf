// Trails: a single MapLibre line layer fed by a GeoJSON source whose
// features are the per-station position histories. Newest position is
// positions[0]; we emit coordinates in time order (newest-first is fine
// since LineString rendering is order-agnostic for visual appearance).
//
// Stations with fewer than 2 positions are skipped (no trail to draw).
// Z-order: MapLibre canvas layers always render below DOM-based markers
// (the stations layer), so the trail line naturally sits beneath the
// station icons without explicit beforeLayerId placement.

const SOURCE_ID = 'gw-trails';
const LAYER_ID = 'gw-trails-line';

export function mountTrailsLayer(map, getStations) {
  if (!map.getSource(SOURCE_ID)) {
    map.addSource(SOURCE_ID, {
      type: 'geojson',
      data: { type: 'FeatureCollection', features: [] },
    });
  }
  if (!map.getLayer(LAYER_ID)) {
    map.addLayer({
      id: LAYER_ID,
      type: 'line',
      source: SOURCE_ID,
      paint: {
        'line-color': '#3fb950',
        'line-width': 2,
        'line-opacity': 0.65,
      },
    });
  }

  function refresh() {
    const stations = getStations();
    if (!stations) return;

    const features = [];
    for (const [callsign, s] of stations) {
      const points = s.positions;
      if (!points || points.length < 2) continue;
      features.push({
        type: 'Feature',
        geometry: {
          type: 'LineString',
          coordinates: points.map((p) => [p.lon, p.lat]),
        },
        properties: { callsign },
      });
    }
    const src = map.getSource(SOURCE_ID);
    if (src) src.setData({ type: 'FeatureCollection', features });
  }

  function destroy() {
    if (map.getLayer(LAYER_ID)) map.removeLayer(LAYER_ID);
    if (map.getSource(SOURCE_ID)) map.removeSource(SOURCE_ID);
  }

  function setVisible(visible) {
    if (map.getLayer(LAYER_ID)) {
      map.setLayoutProperty(LAYER_ID, 'visibility', visible ? 'visible' : 'none');
    }
  }

  return { refresh, destroy, setVisible };
}
