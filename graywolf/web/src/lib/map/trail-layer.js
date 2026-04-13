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
import { esc, timeAgo, fmtLat, fmtLon, viaCls, viaText } from './popup-helpers.js';

const TRAIL_COLOR = '#2b6cb0';
const DOT_FILL = '#ffaa00';
const DOT_RADIUS = 5;
const DOT_WEIGHT = 2;
const HIT_TOLERANCE = 15; // px — generous click/hover zone

export class TrailLayer {
  constructor(map, stationLayer) {
    this.map = map;
    this.stationLayer = stationLayer;
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

        // Visible trail line
        L.polyline([coords[i], coords[i + 1]], {
          color: TRAIL_COLOR,
          weight: 4,
          opacity: Math.max(opacity, 0.3),
        }).addTo(this.layerGroup);

        // Invisible fat hit zone for hover tooltip on the line
        L.polyline([coords[i], coords[i + 1]], {
          weight: HIT_TOLERANCE,
          opacity: 0,
          interactive: true,
        })
          .bindTooltip(s.callsign, {
            permanent: false,
            direction: 'right',
            offset: [8, 0],
            className: 'callsign-label',
            sticky: true,
          })
          .addTo(this.layerGroup);
      }

      // Dots at previous positions (skip positions[0] — that's the station icon)
      for (let i = 1; i < s.positions.length; i++) {
        const p = s.positions[i];
        const opacity = 0.9 - ((i - 1) / segCount) * 0.5;

        const popupContent = _dotPopup(s.callsign, p, this.stationLayer);
        const popupOpts = { className: 'station-popup', maxWidth: 280, minWidth: 200 };
        const tooltipOpts = {
          permanent: false, direction: 'right', offset: [8, 0], className: 'callsign-label',
        };
        const trailKey = `trail:${s.callsign}:${i}`;
        const sl = this.stationLayer;
        // Synthetic station-like object with this position's metadata for path rendering
        const posCtx = { ...s, via: p.via, path: p.path, path_positions: p.path_positions };

        const bindDotEvents = (marker) => {
          marker
            .bindPopup(popupContent, popupOpts)
            .bindTooltip(s.callsign, tooltipOpts);
          marker.on('mouseover', () => sl.showPath(trailKey, posCtx, p));
          marker.on('mouseout', () => {
            if (sl.popupKey !== trailKey) sl.clearPath();
          });
          marker.on('popupopen', (e) => {
            sl.popupKey = trailKey;
            sl.showPath(trailKey, posCtx, p);
            const container = e.popup.getElement();
            if (container) {
              container.addEventListener('click', (ev) => {
                const link = ev.target.closest('.path-link');
                if (!link) return;
                ev.preventDefault();
                sl.focusStation(link.dataset.callsign);
              });
            }
          });
          marker.on('popupclose', () => {
            sl.popupKey = null;
            sl.clearPath();
          });
          return marker;
        };

        // Invisible larger hit area behind the dot for easier clicking
        bindDotEvents(L.circleMarker([p.lat, p.lon], {
          radius: HIT_TOLERANCE,
          opacity: 0,
          fillOpacity: 0,
          interactive: true,
        })).addTo(this.layerGroup);

        // Visible dot on top — also carries popup so direct clicks work
        bindDotEvents(L.circleMarker([p.lat, p.lon], {
          radius: DOT_RADIUS,
          color: TRAIL_COLOR,
          fillColor: DOT_FILL,
          fillOpacity: Math.max(opacity, 0.4),
          opacity: Math.max(opacity, 0.4),
          weight: DOT_WEIGHT,
        })).addTo(this.layerGroup);
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

// Trail dot popup uses per-position metadata (via, path, direction, etc.)
// so it reflects the packet state at the time this position was reported.
function _dotPopup(callsign, pos, stationLayer) {
  const ago = timeAgo(pos.timestamp);
  const dir = pos.direction || 'RX';
  const dirCls = dir === 'RX' ? 'b-rx' : dir === 'TX' ? 'b-tx' : 'b-is';

  let html = `<div class="stn-popup">`;
  html += `<div class="stn-hdr">`;
  html += `<span class="stn-call">${esc(callsign)}</span>`;
  if (dir !== 'IS') {
    html += `<span class="badge ${dirCls}">${esc(dir)}</span>`;
  }
  html += `</div>`;
  html += `<div class="stn-sub">${ago}`;
  if (pos.channel) html += ` &middot; Ch ${pos.channel}`;
  html += `</div>`;
  html += `<div class="stn-sep"></div>`;
  html += `<div class="stn-coords">${fmtLat(pos.lat)} ${fmtLon(pos.lon)}</div>`;

  const meta = [];
  if (pos.speed_kt > 0) meta.push(`${Math.round(pos.speed_kt * 1.15078)}mph`);
  if (pos.course != null) meta.push(`${pos.course}\u00B0`);
  if (pos.has_alt) meta.push(`alt ${Math.round(pos.alt_m * 3.28084)} ft`);
  if (meta.length) html += `<div class="stn-meta">${meta.join(' \u00B7 ')}</div>`;

  html += `<div class="stn-via ${viaCls(pos)}">${viaText(pos)}</div>`;

  if (pos.hops > 0 && pos.path && pos.path.length) {
    const pathHtml = pos.path.map(call => {
      const clean = call.replace('*', '');
      const suffix = call.endsWith('*') ? '*' : '';
      if (stationLayer.hasStation(clean)) {
        return `<a class="path-link" href="#" data-callsign="${esc(clean)}">${esc(clean)}${suffix}</a>`;
      }
      return esc(call);
    }).join(',');
    html += `<div class="stn-path">${pathHtml}</div>`;
  }

  if (pos.comment) {
    html += `<div class="stn-sep"></div>`;
    html += `<div class="stn-comment">${esc(pos.comment)}</div>`;
  }

  html += `</div>`;
  return html;
}
