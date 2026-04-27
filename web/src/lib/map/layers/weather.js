// Weather labels: per-station DOM marker with the legacy Leaflet
// wx-text chip styling. Floats above the station icon, anchored at the
// chip's bottom so its position relative to the station stays stable
// across zoom levels.
//
// Implementation parallels stations.js: keep a Map<callsign, marker>
// in sync with the data store. The weather layer reads s.weather from
// the stations Map (the data store also keeps a separate weather
// SvelteMap, but stations[callsign].weather is the canonical view).
//
// Why DOM over symbol layer: the legacy wx-text chip has a translucent
// dark background, muted-dim text, and a thin border. MapLibre's
// symbol layer can't paint a backing rectangle behind text -- it can
// only halo. A DOM marker reproduces the chip exactly and toggles via
// element.style.display, matching the stations and my-position layers.
//
// Field names match WeatherDTO in pkg/webapi/stations.go: temp_f,
// wind_mph, wind_dir. Unit conversion honors unitsState.isMetric.

import maplibregl from 'maplibre-gl';
import { unitsState } from '../../settings/units-store.svelte.js';

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
  // callsign → { marker, label, lat, lon }
  const markers = new Map();

  function createMarker(label, lat, lon) {
    const root = document.createElement('div');
    root.className = 'wx-label';
    const text = document.createElement('div');
    text.className = 'wx-text';
    text.textContent = label;
    root.appendChild(text);

    // Anchor 'bottom' + small upward offset so the chip floats above
    // the 21x21 station icon (which sits centered on the lat/lon, so
    // its top is ~10.5px above it). 14px offset leaves a hairline gap.
    return new maplibregl.Marker({
      element: root,
      anchor: 'bottom',
      offset: [0, -14],
    }).setLngLat([lon, lat]).addTo(map);
  }

  function refresh() {
    const stations = getStations();
    if (!stations) return;

    const isMetric = unitsState.isMetric;
    const seen = new Set();

    for (const [callsign, s] of stations) {
      if (!s.weather) continue;
      const pos = s.positions && s.positions[0];
      if (!pos) continue;
      const label = formatLabel(s.weather, isMetric);
      if (!label) continue;

      seen.add(callsign);
      const entry = markers.get(callsign);
      if (!entry) {
        const marker = createMarker(label, pos.lat, pos.lon);
        markers.set(callsign, { marker, label, lat: pos.lat, lon: pos.lon });
      } else {
        // Cheap change detection: skip the DOM/setLngLat work when
        // nothing meaningful moved. The text node only updates when
        // the formatted label actually changes (units toggle, fresh
        // weather packet).
        if (entry.lat !== pos.lat || entry.lon !== pos.lon) {
          entry.marker.setLngLat([pos.lon, pos.lat]);
          entry.lat = pos.lat;
          entry.lon = pos.lon;
        }
        if (entry.label !== label) {
          const textEl = entry.marker.getElement().querySelector('.wx-text');
          if (textEl) textEl.textContent = label;
          entry.label = label;
        }
      }
    }

    // Drop markers whose station no longer reports weather (or fell
    // out of the timerange / bbox).
    for (const [callsign, entry] of markers) {
      if (!seen.has(callsign)) {
        entry.marker.remove();
        markers.delete(callsign);
      }
    }
  }

  let visible = true;
  function setVisible(next) {
    visible = !!next;
    const display = visible ? '' : 'none';
    for (const { marker } of markers.values()) {
      marker.getElement().style.display = display;
    }
  }

  // Wrap refresh so newly-minted markers honor the current visibility.
  const wrappedRefresh = () => {
    refresh();
    if (!visible) {
      for (const { marker } of markers.values()) {
        marker.getElement().style.display = 'none';
      }
    }
  };

  function destroy() {
    for (const { marker } of markers.values()) marker.remove();
    markers.clear();
  }

  return { refresh: wrappedRefresh, destroy, setVisible };
}
