// Hover digi-path layer (MapLibre): a single GeoJSON line source whose
// data is repopulated when a station marker is moused over and cleared on
// mouseleave. Mirrors the legacy station-layer.js _showPath() logic:
//
//   station_pos -> each H-bit digi (path entries ending in '*') with a
//   known position (path_positions[i] != [0,0]) -> own_pos (RF stations
//   only, and only if station != own).
//
// The legacy renderer drew a green glow polyline (weight 9, opacity 0.2)
// underneath a crisp polyline (weight 3, opacity 0.9) plus a labeled
// circleMarker at each digi position. We approximate that with two
// MapLibre line layers sharing the same source, plus a circle layer
// driven by a separate "digi nodes" source. The tooltip on each digi
// dot is omitted -- legacy used Leaflet's bindTooltip which has no
// direct MapLibre equivalent on a circle feature; the labels can be
// added later if missed.

const PATH_SRC = 'gw-hover-path';
const PATH_GLOW_LAYER = 'gw-hover-path-glow';
const PATH_LINE_LAYER = 'gw-hover-path-line';
const NODES_SRC = 'gw-hover-path-nodes';
const NODES_LAYER = 'gw-hover-path-nodes';

const POS_EPSILON = 0.00001;

const EMPTY_FC = { type: 'FeatureCollection', features: [] };

export function mountHoverPathLayer(map, getOwnPosition = () => null) {
  if (!map.getSource(PATH_SRC)) {
    map.addSource(PATH_SRC, { type: 'geojson', data: EMPTY_FC });
  }
  if (!map.getSource(NODES_SRC)) {
    map.addSource(NODES_SRC, { type: 'geojson', data: EMPTY_FC });
  }
  if (!map.getLayer(PATH_GLOW_LAYER)) {
    map.addLayer({
      id: PATH_GLOW_LAYER,
      type: 'line',
      source: PATH_SRC,
      layout: { 'line-cap': 'round', 'line-join': 'round' },
      paint: {
        'line-color': '#3fb950',
        'line-width': 9,
        'line-opacity': 0.2,
      },
    });
  }
  if (!map.getLayer(PATH_LINE_LAYER)) {
    map.addLayer({
      id: PATH_LINE_LAYER,
      type: 'line',
      source: PATH_SRC,
      layout: { 'line-cap': 'round', 'line-join': 'round' },
      paint: {
        'line-color': '#3fb950',
        'line-width': 3,
        'line-opacity': 0.9,
      },
    });
  }
  if (!map.getLayer(NODES_LAYER)) {
    map.addLayer({
      id: NODES_LAYER,
      type: 'circle',
      source: NODES_SRC,
      paint: {
        'circle-radius': 5,
        'circle-color': '#1a1e24',
        'circle-stroke-color': '#3fb950',
        'circle-stroke-width': 2,
      },
    });
  }

  function show(station) {
    if (!station) {
      clear();
      return;
    }
    const pos = station.positions && station.positions[0];
    if (!pos) {
      clear();
      return;
    }

    // station -> H-bit digis with known positions
    const coords = [[pos.lon, pos.lat]];
    const nodeFeatures = [];
    const path = station.path || [];
    const pps = station.path_positions || [];
    for (let i = 0; i < path.length; i++) {
      if (!path[i] || !path[i].endsWith('*')) continue;
      const pp = pps[i];
      if (!Array.isArray(pp) || pp.length !== 2) continue;
      if (pp[0] === 0 && pp[1] === 0) continue;
      // pp is [lat, lon] from the server; MapLibre wants [lon, lat]
      coords.push([pp[1], pp[0]]);
      nodeFeatures.push({
        type: 'Feature',
        geometry: { type: 'Point', coordinates: [pp[1], pp[0]] },
        properties: { callsign: path[i].replace('*', '') },
      });
    }

    // RF stations: extend the line to the local "own" position, unless the
    // station IS the own position (epsilon-checked).
    const own = getOwnPosition && getOwnPosition();
    if (station.via === 'rf' && own) {
      const atOwn =
        Math.abs(pos.lat - own.lat) < POS_EPSILON &&
        Math.abs(pos.lon - own.lon) < POS_EPSILON;
      if (!atOwn) {
        coords.push([own.lon, own.lat]);
      }
    }

    if (coords.length < 2) {
      clear();
      return;
    }

    map.getSource(PATH_SRC).setData({
      type: 'FeatureCollection',
      features: [
        {
          type: 'Feature',
          geometry: { type: 'LineString', coordinates: coords },
          properties: {},
        },
      ],
    });
    map.getSource(NODES_SRC).setData({
      type: 'FeatureCollection',
      features: nodeFeatures,
    });
  }

  function clear() {
    const pathSrc = map.getSource(PATH_SRC);
    const nodesSrc = map.getSource(NODES_SRC);
    if (pathSrc) pathSrc.setData(EMPTY_FC);
    if (nodesSrc) nodesSrc.setData(EMPTY_FC);
  }

  function destroy() {
    for (const id of [NODES_LAYER, PATH_LINE_LAYER, PATH_GLOW_LAYER]) {
      if (map.getLayer(id)) map.removeLayer(id);
    }
    for (const id of [NODES_SRC, PATH_SRC]) {
      if (map.getSource(id)) map.removeSource(id);
    }
  }

  return { show, clear, destroy };
}
