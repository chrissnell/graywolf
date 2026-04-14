// Station marker management for the live map.
//
// Manages a Map<string, {marker, station}> of Leaflet markers, reconciling
// against incoming StationDTO arrays from the server. Handles full loads
// (evict absent stations) and delta updates (merge, client-side age check).
// Popup rendering, hover path visualization, and callsign labels.

import L from 'leaflet';
import { aprsIcon } from './aprs-icons.js';
import { esc, timeAgo, fmtLat, fmtLon, viaCls, viaText } from './popup-helpers.js';

const POS_EPSILON = 0.00001;
const MAX_TRAIL_LEN = 200;

// Path styles for hover digi path rendering
const PATH_STYLE = {
  color: '#3fb950',
  weight: 3,
  opacity: 0.9,
};

const PATH_GLOW_STYLE = {
  color: '#3fb950',
  weight: 9,
  opacity: 0.2,
};

export class StationLayer {
  constructor(map) {
    this.map = map;
    this.layerGroup = L.layerGroup().addTo(map);
    this.hoverPathGroup = L.layerGroup().addTo(map);
    this.markers = new Map(); // key → { marker, station }
    this._hoverKey = null;   // key of station whose path is being shown
    this._popupKey = null;   // key of station whose popup is open
    this._ownPos = null;     // {lat, lon} of own station
  }

  // Reconcile markers against incoming station data.
  // isDelta=false: server response is the complete set; evict absent markers.
  // isDelta=true: merge only; client-side age check for stale markers.
  update(stations, isDelta) {
    // Clear hover paths unconditionally to prevent orphaned polylines
    this.hoverPathGroup.clearLayers();
    this._hoverKey = null;

    const incoming = new Set();

    for (const s of stations) {
      const key = s.is_object ? `obj:${s.callsign}` : `stn:${s.callsign}`;
      incoming.add(key);

      const existing = this.markers.get(key);
      if (existing) {
        this._updateMarker(existing, s, isDelta);
      } else {
        this._addMarker(key, s);
      }
    }

    if (!isDelta) {
      // Full load — remove markers not in response
      for (const [key, entry] of this.markers) {
        if (!incoming.has(key)) {
          entry.marker.remove();
          this.markers.delete(key);
        }
      }
    } else {
      // Delta — client-side age check: remove stations older than timerange
      // The caller (LiveMap) sets the timerange; we check last_heard age here.
      // Not done here — the LiveMap component passes the timerange and we
      // handle it in pruneStale() which the caller invokes after update().
    }
  }

  // Remove markers whose last_heard exceeds the given timerange (seconds).
  // Called by LiveMap after delta updates.
  pruneStale(timerangeSec) {
    const cutoff = Date.now() - timerangeSec * 1000;
    for (const [key, entry] of this.markers) {
      const heard = new Date(entry.station.last_heard).getTime();
      if (heard < cutoff) {
        entry.marker.remove();
        this.markers.delete(key);
      }
    }
  }

  // Apply visibility filters from layer toggles (client-side only).
  applyVisibilityFilter(toggles) {
    for (const [, entry] of this.markers) {
      const visible = toggles.stations &&
        (toggles.aprsIs || entry.station.via !== 'is');
      if (visible) {
        if (!this.layerGroup.hasLayer(entry.marker)) {
          this.layerGroup.addLayer(entry.marker);
        }
      } else {
        this.layerGroup.removeLayer(entry.marker);
      }
    }
  }

  // Get station data for trail rendering. Returns iterable of station objects
  // with locally-accumulated positions.
  getStations() {
    return Array.from(this.markers.values(), (e) => e.station);
  }

  destroy() {
    this.layerGroup.clearLayers();
    this.hoverPathGroup.clearLayers();
    this.markers.clear();
  }

  setOwnPosition(lat, lon) {
    this._ownPos = { lat, lon };
  }

  // Show path for own beacon station (called on ownMarker hover)
  showOwnPath() {
    if (!this._ownPos) return;
    for (const [key, entry] of this.markers) {
      const pos = entry.station.positions[0];
      if (Math.abs(pos.lat - this._ownPos.lat) < POS_EPSILON &&
          Math.abs(pos.lon - this._ownPos.lon) < POS_EPSILON) {
        this._showPath(key, entry.station);
        return;
      }
    }
  }

  // Returns true if a station marker exists at the own position.
  hasOwnStation() {
    if (!this._ownPos) return false;
    for (const [, entry] of this.markers) {
      const pos = entry.station.positions[0];
      if (Math.abs(pos.lat - this._ownPos.lat) < POS_EPSILON &&
          Math.abs(pos.lon - this._ownPos.lon) < POS_EPSILON) {
        return true;
      }
    }
    return false;
  }

  clearPath() {
    this._clearPath();
  }

  // Show a digi path overlay for the given key. origin overrides the start
  // position (used by trail dots whose position differs from positions[0]).
  showPath(key, station, origin) {
    this._showPath(key, station, origin);
  }

  // Set/get the key whose popup is currently open, so hover-out doesn't
  // clear the path while a popup is pinned.
  get popupKey() { return this._popupKey; }
  set popupKey(v) { this._popupKey = v; }

  // Check whether a station marker exists for the given callsign.
  hasStation(callsign) {
    return this.markers.has(`stn:${callsign}`);
  }

  // Pan to a station and open its popup. Returns false if not found.
  focusStation(callsign) {
    const entry = this.markers.get(`stn:${callsign}`);
    if (!entry) return false;
    const p = entry.station.positions[0];
    this.map.setView([p.lat, p.lon], this.map.getZoom());
    entry.marker.openPopup();
    return true;
  }

  // --- internal ---

  _addMarker(key, station) {
    const icon = aprsIcon(station.symbol_table, station.symbol_code);
    const pos = station.positions[0];
    const marker = L.marker([pos.lat, pos.lon], { icon });

    // IS station styling: reduced opacity + purple ring
    if (station.via === 'is') {
      marker.setOpacity(0.5);
    }

    // Zoom-gated callsign tooltip
    marker.bindTooltip(station.callsign, {
      permanent: true,
      direction: 'right',
      offset: [12, 0],
      className: 'callsign-label',
    });

    // Popup
    marker.bindPopup(this._popupContent(station), {
      className: 'station-popup',
      maxWidth: 280,
      minWidth: 200,
    });

    // Hover path events
    marker.on('mouseover', () => this._showPath(key, station));
    marker.on('mouseout', () => {
      // Keep path if popup is open for this station
      if (this._popupKey !== key) {
        this._clearPath();
      }
    });
    marker.on('popupopen', (e) => {
      this._popupKey = key;
      this._showPath(key, station);
      // Wire up path callsign links
      const container = e.popup.getElement();
      if (container) {
        container.addEventListener('click', (ev) => {
          const link = ev.target.closest('.path-link');
          if (!link) return;
          ev.preventDefault();
          const callsign = link.dataset.callsign;
          const entry = this.markers.get(`stn:${callsign}`);
          if (entry) {
            const p = entry.station.positions[0];
            this.map.setView([p.lat, p.lon], this.map.getZoom());
            entry.marker.openPopup();
          }
        });
      }
    });
    marker.on('popupclose', () => {
      this._popupKey = null;
      this._clearPath();
    });

    this.layerGroup.addLayer(marker);
    this.markers.set(key, { marker, station });

    marker.openTooltip();
  }

  _updateMarker(entry, station, isDelta) {
    const { marker } = entry;
    const newPos = station.positions[0];

    // Client-side trail merge for delta mode
    if (isDelta && station.positions.length === 1) {
      const oldPositions = entry.station.positions || [];
      if (oldPositions.length > 0) {
        const prev = oldPositions[0];
        if (Math.abs(newPos.lat - prev.lat) > POS_EPSILON ||
            Math.abs(newPos.lon - prev.lon) > POS_EPSILON) {
          // New position differs — prepend and cap
          station.positions = [newPos, ...oldPositions].slice(0, MAX_TRAIL_LEN);
        } else {
          // Same position — keep existing trail, update timestamp
          station.positions = [newPos, ...oldPositions.slice(1)];
        }
      }
    }

    // Move marker
    marker.setLatLng([newPos.lat, newPos.lon]);

    // Update icon if symbol changed
    if (station.symbol_table !== entry.station.symbol_table ||
        station.symbol_code !== entry.station.symbol_code) {
      marker.setIcon(aprsIcon(station.symbol_table, station.symbol_code));
    }

    // Update IS styling
    marker.setOpacity(station.via === 'is' ? 0.5 : 1);

    // Update tooltip text if callsign changed (shouldn't happen, but safe)
    if (station.callsign !== entry.station.callsign) {
      marker.setTooltipContent(station.callsign);
    }

    // Update popup content
    marker.setPopupContent(this._popupContent(station));

    // Update hover path handler with fresh station data
    marker.off('mouseover');
    marker.off('mouseout');
    const key = station.is_object ? `obj:${station.callsign}` : `stn:${station.callsign}`;
    marker.on('mouseover', () => this._showPath(key, station));
    marker.on('mouseout', () => {
      if (this._popupKey !== key) this._clearPath();
    });

    entry.station = station;
  }

  _showPath(key, station, origin) {
    if (this._hoverKey === key) return; // already showing
    this.hoverPathGroup.clearLayers();
    this._hoverKey = key;

    const { path, path_positions } = station;
    const startPos = origin || station.positions[0];

    // Build chain: station → digis (H-bit, known positions) → own position
    const points = [[startPos.lat, startPos.lon]];

    if (path && path_positions) {
      for (let i = 0; i < path.length; i++) {
        if (!path[i].endsWith('*')) continue;
        const pp = path_positions[i];
        if (!pp || (pp[0] === 0 && pp[1] === 0)) continue;
        points.push([pp[0], pp[1]]);
      }
    }

    // End at own position for RF stations (skip if station IS at own position)
    if (station.via === 'rf' && this._ownPos) {
      const atOwn = Math.abs(startPos.lat - this._ownPos.lat) < POS_EPSILON &&
                    Math.abs(startPos.lon - this._ownPos.lon) < POS_EPSILON;
      if (!atOwn) {
        points.push([this._ownPos.lat, this._ownPos.lon]);
      }
    }

    if (points.length < 2) return;

    // Glow layer, then crisp line on top
    L.polyline(points, PATH_GLOW_STYLE).addTo(this.hoverPathGroup);
    L.polyline(points, PATH_STYLE).addTo(this.hoverPathGroup);

    // Mark digi positions with labeled dots
    if (path && path_positions) {
      for (let i = 0; i < path.length; i++) {
        if (!path[i].endsWith('*')) continue;
        const pp = path_positions[i];
        if (!pp || (pp[0] === 0 && pp[1] === 0)) continue;
        L.circleMarker([pp[0], pp[1]], {
          radius: 5,
          color: '#3fb950',
          fillColor: '#1a1e24',
          fillOpacity: 1,
          weight: 2,
        }).bindTooltip(path[i].replace('*', ''), {
          permanent: false,
          direction: 'right',
          className: 'callsign-label',
        }).addTo(this.hoverPathGroup);
      }
    }
  }

  _clearPath() {
    this.hoverPathGroup.clearLayers();
    this._hoverKey = null;
  }

  _popupContent(s) {
    const pos = s.positions[0];
    const ago = timeAgo(s.last_heard);
    const dirCls = s.direction === 'RX' ? 'b-rx' : s.direction === 'TX' ? 'b-tx' : 'b-is';

    let html = `<div class="stn-popup">`;
    html += `<div class="stn-hdr">`;
    html += `<span class="stn-call">${esc(s.callsign)}</span>`;
    if (s.direction !== 'IS') {
      html += `<span class="badge ${dirCls}">${esc(s.direction)}</span>`;
    }
    html += `</div>`;
    html += `<div class="stn-sub">${ago} &middot; Ch ${s.channel}</div>`;
    html += `<div class="stn-sep"></div>`;
    html += `<div class="stn-coords">${fmtLat(pos.lat)} ${fmtLon(pos.lon)}</div>`;
    const meta = [];
    if (pos.speed_kt > 0) meta.push(`${Math.round(pos.speed_kt * 1.15078)}mph`);
    if (pos.course != null) meta.push(`${pos.course}\u00B0`);
    if (pos.has_alt) meta.push(`alt ${Math.round(pos.alt_m * 3.28084)} ft`);
    if (meta.length) html += `<div class="stn-meta">${meta.join(' \u00B7 ')}</div>`;
    html += `<div class="stn-via ${viaCls(s)}">${viaText(s)}</div>`;
    if (s.hops > 0 && s.path && s.path.length) {
      const pathHtml = s.path.map(call => {
        const clean = call.replace('*', '');
        const suffix = call.endsWith('*') ? '*' : '';
        const key = `stn:${clean}`;
        if (this.markers.has(key)) {
          return `<a class="path-link" href="#" data-callsign="${esc(clean)}">${esc(clean)}${suffix}</a>`;
        }
        return esc(call);
      }).join(',');
      html += `<div class="stn-path">${pathHtml}</div>`;
    }
    if (s.comment) {
      html += `<div class="stn-sep"></div>`;
      html += `<div class="stn-comment">${esc(s.comment)}</div>`;
    }
    html += `</div>`;
    return html;
  }
}
