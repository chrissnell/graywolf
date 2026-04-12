// Polyline trails for moving stations.
//
// Draws position history as polylines for stations with positions.length > 1.
// Static stations (len 1) are skipped. Trail data comes from StationLayer's
// locally-accumulated positions (full trail on initial load, plus client-side
// merged positions from delta updates).

import L from 'leaflet';

// Per-station color palette — deterministic hash to color index
const TRAIL_COLORS = [
  '#58a6ff', '#3fb950', '#d29922', '#f85149',
  '#bc8cff', '#79c0ff', '#56d364', '#e3b341',
];

function colorFor(callsign) {
  let hash = 0;
  for (let i = 0; i < callsign.length; i++) {
    hash = ((hash << 5) - hash + callsign.charCodeAt(i)) | 0;
  }
  return TRAIL_COLORS[Math.abs(hash) % TRAIL_COLORS.length];
}

export class TrailLayer {
  constructor(map) {
    this.map = map;
    this.layerGroup = L.layerGroup();
    this._visible = false;
  }

  // Draw/redraw trails for all stations with movement history.
  // stations: array of station objects with positions arrays.
  update(stations) {
    this.layerGroup.clearLayers();

    for (const s of stations) {
      if (!s.positions || s.positions.length < 2) continue;

      const color = colorFor(s.callsign);
      const coords = s.positions.map((p) => [p.lat, p.lon]);

      // Fading opacity: full opacity for newest segment, reduced for older
      const segCount = coords.length - 1;
      for (let i = 0; i < segCount; i++) {
        const opacity = 0.8 - (i / segCount) * 0.6; // 0.8 → 0.2
        L.polyline([coords[i], coords[i + 1]], {
          color,
          weight: 2,
          opacity: Math.max(opacity, 0.2),
        }).addTo(this.layerGroup);
      }
    }
  }

  show() {
    if (!this._visible) {
      this.layerGroup.addTo(this.map);
      this._visible = true;
    }
  }

  hide() {
    if (this._visible) {
      this.layerGroup.remove();
      this._visible = false;
    }
  }

  destroy() {
    this.layerGroup.clearLayers();
    if (this._visible) this.layerGroup.remove();
  }
}
