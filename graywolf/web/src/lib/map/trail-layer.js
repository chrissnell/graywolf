// Polyline trails for moving stations.
//
// Draws position history as polylines for stations with positions.length > 1.
// Static stations (len 1) are skipped. Trail data comes from StationLayer's
// locally-accumulated positions (full trail on initial load, plus client-side
// merged positions from delta updates).
//
// Each previous position gets a clickable dot showing packet details.
// The current position (positions[0]) is NOT dotted — it uses the station icon.

import L from 'leaflet';

const TRAIL_COLOR = '#2b6cb0';
const DOT_FILL = '#ffaa00';
const DOT_RADIUS = 5;
const DOT_WEIGHT = 2;

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

      // Dots at previous positions (skip positions[0] — that's the station icon)
      for (let i = 1; i < s.positions.length; i++) {
        const p = s.positions[i];
        const opacity = 0.9 - ((i - 1) / segCount) * 0.5;

        L.circleMarker([p.lat, p.lon], {
          radius: DOT_RADIUS,
          color: TRAIL_COLOR,
          fillColor: DOT_FILL,
          fillOpacity: Math.max(opacity, 0.4),
          opacity: Math.max(opacity, 0.4),
          weight: DOT_WEIGHT,
        })
          .bindPopup(_dotPopup(s.callsign, p), {
            className: 'station-popup',
            maxWidth: 280,
            minWidth: 180,
          })
          .bindTooltip(s.callsign, {
            permanent: false,
            direction: 'right',
            offset: [8, 0],
            className: 'callsign-label',
          })
          .addTo(this.layerGroup);
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

// --- popup helpers ---

function _dotPopup(callsign, pos) {
  const ago = _timeAgo(pos.timestamp);
  const latDir = pos.lat >= 0 ? 'N' : 'S';
  const lonDir = pos.lon >= 0 ? 'E' : 'W';
  const lat = `${Math.abs(pos.lat).toFixed(4)}\u00B0${latDir}`;
  const lon = `${Math.abs(pos.lon).toFixed(4)}\u00B0${lonDir}`;

  let html = `<div class="stn-popup">`;
  html += `<div class="stn-hdr"><span class="stn-call">${_esc(callsign)}</span></div>`;
  html += `<div class="stn-sub">${ago}</div>`;
  html += `<div class="stn-sep"></div>`;
  html += `<div class="stn-coords">${lat} ${lon}</div>`;

  const meta = [];
  if (pos.speed_kt > 0) meta.push(`${Math.round(pos.speed_kt * 1.15078)}mph`);
  if (pos.course != null) meta.push(`${pos.course}\u00B0`);
  if (pos.has_alt) meta.push(`alt ${Math.round(pos.alt_m * 3.28084)} ft`);
  if (meta.length) html += `<div class="stn-meta">${meta.join(' \u00B7 ')}</div>`;

  html += `</div>`;
  return html;
}

function _esc(str) {
  if (!str) return '';
  return str.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
}

function _timeAgo(isoStr) {
  const ms = Date.now() - new Date(isoStr).getTime();
  const sec = Math.floor(ms / 1000);
  if (sec < 60) return `${sec}s ago`;
  const min = Math.floor(sec / 60);
  if (min < 60) return `${min} min ago`;
  const hr = Math.floor(min / 60);
  return `${hr}h ${min % 60}m ago`;
}
