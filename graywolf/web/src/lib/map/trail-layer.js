// Polyline trails for moving stations.
//
// Draws position history as polylines for stations with positions.length > 1.
// Static stations (len 1) are skipped. Trail data comes from StationLayer's
// locally-accumulated positions (full trail on initial load, plus client-side
// merged positions from delta updates).

import L from 'leaflet';

const TRAIL_COLOR = '#e05050';

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

      const coords = s.positions.map((p) => [p.lat, p.lon]);

      // Fading opacity: full opacity for newest segment, reduced for older
      const segCount = coords.length - 1;
      for (let i = 0; i < segCount; i++) {
        const opacity = 0.9 - (i / segCount) * 0.6; // 0.9 → 0.3
        L.polyline([coords[i], coords[i + 1]], {
          color: TRAIL_COLOR,
          weight: 4,
          opacity: Math.max(opacity, 0.3),
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
