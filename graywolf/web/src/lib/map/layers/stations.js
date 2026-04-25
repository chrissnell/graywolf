// Stations layer (MapLibre): keeps a Map<callsign, maplibregl.Marker> in
// sync with the data store's stations Map. Each marker is an HTML element
// (APRS icon + callsign label) attached via maplibregl.Marker.
//
// refresh() is the imperative entry point; LiveMapV2 wires it to a $effect
// that tracks the data-store's stations Map. New callsigns get fresh
// markers, existing markers get setLngLat, dropped callsigns get removed.
//
// Symbol changes: in practice a station's symbol_table/symbol_code is
// stable across the lifetime of a session, so we skip change-detection
// here to keep refresh() O(n) and allocation-free for the common path.
// If a symbol does change mid-session the marker keeps its old icon
// until the next page load -- acceptable for now.

import maplibregl from 'maplibre-gl';
import { createAprsIconElement } from '../aprs-icon-element.js';

export function mountStationsLayer(map, getStations) {
  // callsign → { marker }
  const markers = new Map();

  function createRoot(s) {
    const root = document.createElement('div');
    root.className = 'gw-station-marker';

    const icon = createAprsIconElement({
      table: s.symbol_table,
      symbol: s.symbol_code,
      displayPx: 28,
    });
    root.appendChild(icon);

    const label = document.createElement('div');
    label.className = 'gw-station-label';
    label.textContent = s.callsign;
    root.appendChild(label);

    return root;
  }

  function refresh() {
    const stations = getStations();
    if (!stations) return;

    const seen = new Set();
    for (const [callsign, s] of stations) {
      seen.add(callsign);
      const pos = s.positions && s.positions[0];
      if (!pos) continue;

      const entry = markers.get(callsign);
      if (!entry) {
        const root = createRoot(s);
        const marker = new maplibregl.Marker({ element: root, anchor: 'bottom' })
          .setLngLat([pos.lon, pos.lat])
          .addTo(map);
        markers.set(callsign, { marker });
      } else {
        entry.marker.setLngLat([pos.lon, pos.lat]);
      }
    }

    // Drop markers whose callsign disappeared (timerange prune, bbox change).
    for (const [callsign, entry] of markers) {
      if (!seen.has(callsign)) {
        entry.marker.remove();
        markers.delete(callsign);
      }
    }
  }

  function destroy() {
    for (const { marker } of markers.values()) marker.remove();
    markers.clear();
  }

  return { refresh, destroy };
}
