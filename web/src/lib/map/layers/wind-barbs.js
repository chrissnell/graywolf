// Wind-barb layer (MapLibre): per-station DOM marker carrying a standard
// meteorological wind barb rendered as inline SVG. Replaces the old
// text-based wind speed/direction annotation that used to live in the
// weather chip -- the barb now carries both, oriented to wind direction
// and encoding sustained speed with the conventional WMO barbs/pennants.
//
// Architecture mirrors weather.js / stations.js: keep a
// Map<callsign, entry> in sync with the data store, expose an imperative
// refresh() plus setVisible()/setFilter()/destroy(). Each marker is a
// maplibregl.Marker wrapping an inert (pointer-events:none) <svg>, so the
// station icon underneath keeps its click/hover behavior.
//
// The barb glyph math lives in wind-barb-glyph.js (pure, unit-tested).
// Field names match WeatherDTO in pkg/webapi/stations.go: wind_mph,
// wind_dir.

import maplibregl from 'maplibre-gl';
import { buildWindBarb } from './wind-barb-glyph.js';

export function mountWindBarbsLayer(map, getStations) {
  // callsign → { marker, svg, mph, dir, lat, lon }
  const markers = new Map();

  function makeSvg(inner) {
    const ns = 'http://www.w3.org/2000/svg';
    const svg = document.createElementNS(ns, 'svg');
    svg.setAttribute('class', 'wb-svg');
    svg.setAttribute('viewBox', '-55 -55 110 110');
    svg.setAttribute('width', '55');
    svg.setAttribute('height', '55');
    svg.innerHTML = inner;
    return svg;
  }

  function createMarker(inner, lat, lon) {
    const root = document.createElement('div');
    root.className = 'wb-marker';
    const svg = makeSvg(inner);
    root.appendChild(svg);
    const marker = new maplibregl.Marker({ element: root, anchor: 'center' })
      .setLngLat([lon, lat])
      .addTo(map);
    return { marker, svg };
  }

  let filter = null;

  function refresh() {
    const stations = getStations();
    if (!stations) return;

    const seen = new Set();

    for (const [callsign, s] of stations) {
      if (filter && !filter(s)) continue;
      const wx = s.weather;
      if (!wx || wx.wind_mph == null || wx.wind_dir == null) continue;
      const pos = s.positions && s.positions[0];
      if (!pos) continue;

      const inner = buildWindBarb(wx.wind_mph, wx.wind_dir);
      if (!inner) continue;

      seen.add(callsign);
      const entry = markers.get(callsign);
      if (!entry) {
        const { marker, svg } = createMarker(inner, pos.lat, pos.lon);
        markers.set(callsign, {
          marker,
          svg,
          mph: wx.wind_mph,
          dir: wx.wind_dir,
          lat: pos.lat,
          lon: pos.lon,
        });
      } else {
        if (entry.lat !== pos.lat || entry.lon !== pos.lon) {
          entry.marker.setLngLat([pos.lon, pos.lat]);
          entry.lat = pos.lat;
          entry.lon = pos.lon;
        }
        // Only rebuild the SVG when the wind actually changed.
        if (entry.mph !== wx.wind_mph || entry.dir !== wx.wind_dir) {
          entry.svg.innerHTML = inner;
          entry.mph = wx.wind_mph;
          entry.dir = wx.wind_dir;
        }
      }
    }

    for (const [callsign, entry] of markers) {
      if (!seen.has(callsign)) {
        entry.marker.remove();
        markers.delete(callsign);
      }
    }
  }

  let visible = true;
  function applyVisibility() {
    const display = visible ? '' : 'none';
    for (const { marker } of markers.values()) {
      marker.getElement().style.display = display;
    }
  }
  function setVisible(next) {
    visible = !!next;
    applyVisibility();
  }

  function setFilter(pred) {
    filter = typeof pred === 'function' ? pred : null;
    wrappedRefresh();
  }

  function destroy() {
    for (const { marker } of markers.values()) marker.remove();
    markers.clear();
  }

  const wrappedRefresh = () => {
    refresh();
    applyVisibility();
  };

  return { refresh: wrappedRefresh, destroy, setVisible, setFilter };
}
