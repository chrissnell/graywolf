// Weather labels for stations whose latest packet included weather
// telemetry. Renders as a MapLibre symbol layer; canvas symbols sit
// below the DOM-based station markers, and we offset the text upward
// so it shows above the station icon (matching the legacy Leaflet
// overlay's iconAnchor: [40, -14]).
//
// Field names match WeatherDTO in pkg/webapi/stations.go: temp_f,
// wind_mph, wind_dir. Unit conversion honors unitsState.isMetric.

import { unitsState } from '../../settings/units-store.svelte.js';

const SOURCE_ID = 'gw-weather';
const LAYER_ID = 'gw-weather-labels';
const KMH_PER_MPH = 1.60934;

function cardinal(deg) {
  const dirs = ['N', 'NE', 'E', 'SE', 'S', 'SW', 'W', 'NW'];
  return dirs[Math.round(deg / 45) % 8];
}

function formatLabel(wx, isMetric) {
  if (!wx) return '';
  const parts = [];
  if (wx.temp_f != null) {
    const t = isMetric ? ((wx.temp_f - 32) * 5) / 9 : wx.temp_f;
    parts.push(`${Math.round(t)}°${isMetric ? 'C' : 'F'}`);
  }
  if (wx.wind_mph != null) {
    const s = isMetric ? wx.wind_mph * KMH_PER_MPH : wx.wind_mph;
    let wind = `${Math.round(s)}${isMetric ? 'km/h' : 'mph'}`;
    if (wx.wind_dir != null) wind = `${wind} ${cardinal(wx.wind_dir)}`;
    parts.push(wind);
  }
  return parts.join(' ');
}

export function mountWeatherLayer(map, getStations) {
  if (!map.getSource(SOURCE_ID)) {
    map.addSource(SOURCE_ID, {
      type: 'geojson',
      data: { type: 'FeatureCollection', features: [] },
    });
  }
  if (!map.getLayer(LAYER_ID)) {
    map.addLayer({
      id: LAYER_ID,
      type: 'symbol',
      source: SOURCE_ID,
      layout: {
        'text-field': ['get', 'label'],
        'text-size': 11,
        'text-anchor': 'bottom',
        // Lift the label above the station icon. Negative Y in
        // text-offset means upward in MapLibre's symbol coordinates.
        'text-offset': [0, -2.4],
        'text-allow-overlap': true,
        'text-ignore-placement': true,
      },
      paint: {
        'text-color': '#ffffff',
        'text-halo-color': 'rgba(0, 0, 0, 0.7)',
        'text-halo-width': 1.2,
      },
    });
  }

  function refresh() {
    const stations = getStations();
    if (!stations) return;

    const isMetric = unitsState.isMetric;
    const features = [];
    for (const [callsign, s] of stations) {
      if (!s.weather) continue;
      const pos = s.positions && s.positions[0];
      if (!pos) continue;
      const label = formatLabel(s.weather, isMetric);
      if (!label) continue;
      features.push({
        type: 'Feature',
        geometry: { type: 'Point', coordinates: [pos.lon, pos.lat] },
        properties: { callsign, label },
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
