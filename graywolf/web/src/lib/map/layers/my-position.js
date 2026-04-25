// My-position layer: a single maplibregl.Marker tracking the
// operator's own beacon position. Fetched once at start by the data
// store; updated reactively if the data store ever refreshes it.
//
// Visual mirrors the legacy own-position-marker dot styling. The
// element gets the `own-position-marker` and `own-position` classes
// so the existing global CSS in LiveMap.svelte's <style> block (which
// will be carried over to LiveMapV2 in task 28/31) applies.

import maplibregl from 'maplibre-gl';

export function mountMyPositionLayer(map, getMyPosition, {
  onMarkerEnter = null,
  onMarkerLeave = null,
} = {}) {
  let marker = null;
  let lastKey = null;

  function refresh() {
    const p = getMyPosition();
    if (!p || typeof p.lat !== 'number' || typeof p.lon !== 'number') {
      if (marker) { marker.remove(); marker = null; lastKey = null; }
      return;
    }
    const key = `${p.lat},${p.lon}`;
    if (marker && lastKey === key) return;

    if (!marker) {
      const root = document.createElement('div');
      root.className = 'own-position-marker';
      root.title = 'My Position';
      const dot = document.createElement('div');
      dot.className = 'own-position';
      root.appendChild(dot);
      if (onMarkerEnter) root.addEventListener('mouseenter', onMarkerEnter);
      if (onMarkerLeave) root.addEventListener('mouseleave', onMarkerLeave);

      marker = new maplibregl.Marker({ element: root, anchor: 'center' })
        .setLngLat([p.lon, p.lat])
        .addTo(map);
    } else {
      marker.setLngLat([p.lon, p.lat]);
    }
    lastKey = key;
  }

  return {
    refresh,
    destroy() {
      if (marker) { marker.remove(); marker = null; }
    },
  };
}
