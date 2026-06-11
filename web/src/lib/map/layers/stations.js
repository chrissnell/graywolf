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
      displayPx: 21,
    });
    icon.classList.add('gw-station-icon');
    root.appendChild(icon);

    // Right-of-icon column: callsign on top, temperature chip beneath it.
    // align-items:flex-end (CSS) right-justifies the temp to the callsign.
    // The temp slot is filled by the weather layer (see weather.js) and
    // stays hidden until then, so stations without weather show nothing.
    const aside = document.createElement('div');
    aside.className = 'gw-station-aside';

    const label = document.createElement('div');
    label.className = 'gw-station-label';
    label.textContent = s.callsign;
    aside.appendChild(label);

    const temp = document.createElement('div');
    temp.className = 'wx-temp';
    temp.style.display = 'none';
    aside.appendChild(temp);

    root.appendChild(aside);

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
        // anchor:'center' centers the icon square on the lat/lon. The
        // callsign label is absolutely positioned to the right of the
        // icon (see CSS in LiveMapV2.svelte) so it doesn't shift the
        // anchor point.
        const marker = new maplibregl.Marker({ element: root, anchor: 'center' })
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
  // Optional per-station predicate. A station with predicate(s)===false
  // is hidden even when the layer is "visible". Used by Direct RX toggle.
  let filter = null;

  function isAllowed(callsign) {
    if (!filter) return true;
    const s = lookupStation(callsign);
    return !!(s && filter(s));
  }
  function applyDisplay() {
    for (const [callsign, { marker }] of markers) {
      const show = visible && isAllowed(callsign);
      marker.getElement().style.display = show ? '' : 'none';
    }
  }
  function setVisible(next) {
    visible = !!next;
    applyDisplay();
  }
  function setFilter(pred) {
    filter = typeof pred === 'function' ? pred : null;
    applyDisplay();
  }
  // Wrap refresh so newly-minted markers honor current visibility + filter.
  // Hand the weather layer the temp slot inside a station's marker so it
  // can write the temperature chip there (null if the marker isn't up).
  function getTempSlot(callsign) {
    const entry = markers.get(callsign);
    if (!entry) return null;
    return entry.marker.getElement().querySelector('.wx-temp');
  }

  const wrappedRefresh = () => {
    refresh();
    applyDisplay();
  };

  return { refresh: wrappedRefresh, destroy, setVisible, setFilter, getTempSlot };
}
