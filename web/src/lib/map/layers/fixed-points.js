// Fixed-points layer (MapLibre): keeps a Map<id, maplibregl.Marker> in
// sync with the user's fixed-points store. Each marker is an HTML element
// (APRS icon + name label) — visually consistent with station markers, so
// landmarks read like beacons on the map. Mirrors stations.js, minus the
// hover/path plumbing those markers carry.
//
// refresh() is the imperative entry point; LiveMapV2 wires it to a $effect
// that tracks the store's points array. New ids get fresh markers, dropped
// ids get removed. Clicking a marker calls onMarkerClick(point, lngLat) so
// the parent can offer deletion.

import maplibregl from 'maplibre-gl';
import { createAprsIconElement } from '../aprs-icon-element.js';

export function mountFixedPointsLayer(map, getPoints, { onMarkerClick = null } = {}) {
  // id → { marker }
  const markers = new Map();

  function createRoot(p) {
    const root = document.createElement('div');
    root.className = 'gw-fixed-marker';
    root.title = p.name;

    const icon = createAprsIconElement({
      table: p.table,
      symbol: p.symbol,
      overlay: p.overlay || null,
      displayPx: 21,
    });
    icon.classList.add('gw-fixed-icon');
    root.appendChild(icon);

    if (p.name) {
      const label = document.createElement('div');
      label.className = 'gw-fixed-label';
      label.textContent = p.name;
      root.appendChild(label);
    }

    if (onMarkerClick) {
      root.addEventListener('click', (ev) => {
        ev.stopPropagation();
        onMarkerClick(p, { lng: p.lon, lat: p.lat });
      });
    }

    return root;
  }

  function refresh() {
    const points = getPoints();
    if (!points) return;

    const seen = new Set();
    for (const p of points) {
      if (!Number.isFinite(p.lat) || !Number.isFinite(p.lon)) continue;
      seen.add(p.id);
      if (!markers.has(p.id)) {
        const root = createRoot(p);
        const marker = new maplibregl.Marker({ element: root, anchor: 'center' })
          .setLngLat([p.lon, p.lat])
          .addTo(map);
        markers.set(p.id, { marker });
      }
      // Points are immutable once added (no in-place edit), so existing
      // markers need no position/label update.
    }

    // Drop markers whose point was removed.
    for (const [id, entry] of markers) {
      if (!seen.has(id)) {
        entry.marker.remove();
        markers.delete(id);
      }
    }
  }

  let visible = true;
  function applyDisplay() {
    for (const { marker } of markers.values()) {
      marker.getElement().style.display = visible ? '' : 'none';
    }
  }
  function setVisible(next) {
    visible = !!next;
    applyDisplay();
  }

  const wrappedRefresh = () => {
    refresh();
    applyDisplay();
  };

  return {
    refresh: wrappedRefresh,
    setVisible,
    destroy() {
      for (const { marker } of markers.values()) marker.remove();
      markers.clear();
    },
  };
}
