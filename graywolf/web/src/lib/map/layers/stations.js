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
//
// Mouse callbacks (onMarkerEnter/Leave/Click) are wired once per marker
// at creation time. The closure-captured station reference is fine for
// hover (the digi path doesn't change second-to-second) but the click
// handler resolves the FRESHEST station from getStations() at click
// time so the popup doesn't render stale path/comment data.

import maplibregl from 'maplibre-gl';
import { createAprsIconElement } from '../aprs-icon-element.js';

export function mountStationsLayer(map, getStations, {
  onMarkerEnter = null,
  onMarkerLeave = null,
  onMarkerClick = null,
} = {}) {
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

    if (onMarkerEnter) {
      root.addEventListener('mouseenter', () => {
        const fresh = lookupStation(s.callsign) || s;
        onMarkerEnter(fresh);
      });
    }
    if (onMarkerLeave) {
      root.addEventListener('mouseleave', () => onMarkerLeave());
    }
    if (onMarkerClick) {
      root.addEventListener('click', (ev) => {
        ev.stopPropagation();
        const fresh = lookupStation(s.callsign) || s;
        onMarkerClick(fresh);
      });
    }

    return root;
  }

  function lookupStation(callsign) {
    const stations = getStations();
    if (!stations) return null;
    return stations.get(callsign) || null;
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

  // setVisible: toggle marker DOM visibility without removing the
  // markers (so refresh() can keep updating positions in the background).
  // We track the desired state so newly-created markers in subsequent
  // refresh() calls inherit the right visibility.
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

  return { refresh: wrappedRefresh, destroy, setVisible };
}
