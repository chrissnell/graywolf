// NEXRAD radar overlay layer for the Live Map.
//
// Backend-agnostic: it asks radar-source.js for a provider descriptor and
// performs the MapLibre source/layer calls. Raster today, GRA-48 vector
// tomorrow -- this file does not change when the backend flips; only
// ACTIVE_RADAR_BACKEND in radar-source.js does.
//
// Mirrors the other layer modules (stations.js, weather.js): mount returns
// control methods; LiveMapV2 persists settings and drives them via effects.

import { radarProvider } from '../sources/radar-source.js';

export function mountRadarLayer(map, { visible, opacity }) {
  const provider = radarProvider();

  // Insert below the first symbol layer so basemap labels stay readable above
  // the radar. DOM-based layers (stations, weather markers) always render
  // above the GL canvas regardless of GL layer order.
  const firstSymbolId = map.getStyle().layers.find((l) => l.type === 'symbol')?.id;

  map.addSource(provider.sourceId, provider.source);

  for (const layer of provider.layers) {
    const spec = {
      ...layer,
      layout: { ...(layer.layout ?? {}), visibility: visible ? 'visible' : 'none' },
      paint: { ...(layer.paint ?? {}), [provider.opacity.property]: opacity },
    };
    map.addLayer(spec, firstSymbolId);
  }

  function setVisible(v) {
    const value = v ? 'visible' : 'none';
    for (const layer of provider.layers) {
      map.setLayoutProperty(layer.id, 'visibility', value);
    }
  }

  function setOpacity(v) {
    for (const id of provider.opacity.layerIds) {
      map.setPaintProperty(id, provider.opacity.property, v);
    }
  }

  function destroy() {
    for (const layer of provider.layers) {
      if (map.getLayer(layer.id)) map.removeLayer(layer.id);
    }
    if (map.getSource(provider.sourceId)) map.removeSource(provider.sourceId);
  }

  return { setVisible, setOpacity, destroy };
}
