// Station marker management for the live map.
//
// Manages a Map<string, {marker, station}> of Leaflet markers, reconciling
// against incoming StationDTO arrays from the server. Handles full loads
// (evict absent stations) and delta updates (merge, client-side age check).
// Popup rendering, hover path visualization, and zoom-gated callsign labels.

import L from 'leaflet';
import { aprsIcon } from './aprs-icons.js';

const LABEL_ZOOM_THRESHOLD = 10;
const POS_EPSILON = 0.00001;
const MAX_TRAIL_LEN = 200;

// Path style for hover digi path rendering
const PATH_STYLE = {
  color: 'rgba(212, 160, 64, 0.6)',
  weight: 2,
  dashArray: '6,4',
};

export class StationLayer {
  constructor(map) {
    this.map = map;
    this.layerGroup = L.layerGroup().addTo(map);
    this.hoverPathGroup = L.layerGroup().addTo(map);
    this.markers = new Map(); // key → { marker, station }
    this._labelsVisible = map.getZoom() >= LABEL_ZOOM_THRESHOLD;
    this._hoverKey = null;   // key of station whose path is being shown
    this._popupKey = null;   // key of station whose popup is open

    map.on('zoomend', () => this._updateLabelVisibility());
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
    marker.on('popupopen', () => {
      this._popupKey = key;
      this._showPath(key, station);
    });
    marker.on('popupclose', () => {
      this._popupKey = null;
      this._clearPath();
    });

    this.layerGroup.addLayer(marker);
    this.markers.set(key, { marker, station });

    // Manage tooltip visibility based on current zoom
    if (this._labelsVisible) {
      marker.openTooltip();
    } else {
      marker.closeTooltip();
    }
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

  _updateLabelVisibility() {
    const shouldShow = this.map.getZoom() >= LABEL_ZOOM_THRESHOLD;
    if (shouldShow === this._labelsVisible) return;
    this._labelsVisible = shouldShow;

    for (const [, entry] of this.markers) {
      if (shouldShow) {
        entry.marker.openTooltip();
      } else {
        entry.marker.closeTooltip();
      }
    }
  }

  _showPath(key, station) {
    if (this._hoverKey === key) return; // already showing
    this.hoverPathGroup.clearLayers();
    this._hoverKey = key;

    const { path, path_positions } = station;
    if (!path || !path_positions || path.length === 0) return;

    const stationPos = station.positions[0];
    // Build chain of known positions: start from source → digis → station
    // We only draw segments between consecutive known positions.
    const points = [];
    for (let i = 0; i < path.length; i++) {
      if (!path[i].endsWith('*')) continue; // only H-bit digis
      const pp = path_positions[i];
      if (!pp || (pp[0] === 0 && pp[1] === 0)) continue; // unknown digi
      points.push([pp[0], pp[1]]);
    }
    // End at the station itself
    points.push([stationPos.lat, stationPos.lon]);

    if (points.length < 2) return;

    // Draw segments between consecutive known points
    for (let i = 0; i < points.length - 1; i++) {
      L.polyline([points[i], points[i + 1]], PATH_STYLE)
        .addTo(this.hoverPathGroup);
    }
  }

  _clearPath() {
    this.hoverPathGroup.clearLayers();
    this._hoverKey = null;
  }

  _popupContent(s) {
    const pos = s.positions[0];
    const ago = _timeAgo(s.last_heard);
    const dirCls = s.direction === 'RX' ? 'b-rx' : s.direction === 'TX' ? 'b-tx' : 'b-is';

    let html = `<div class="stn-popup">`;
    // Header: callsign + direction badge
    html += `<div class="stn-hdr">`;
    html += `<span class="stn-call">${_esc(s.callsign)}</span>`;
    html += `<span class="badge ${dirCls}">${_esc(s.direction)}</span>`;
    html += `</div>`;
    // Subheader: time ago + channel
    html += `<div class="stn-sub">${ago} &middot; Ch ${s.channel}</div>`;
    // Separator
    html += `<div class="stn-sep"></div>`;
    // Coordinates
    html += `<div class="stn-coords">${_fmtLat(pos.lat)} ${_fmtLon(pos.lon)}</div>`;
    // Speed/course/alt
    const meta = [];
    if (pos.speed_kt > 0) meta.push(`${Math.round(pos.speed_kt)}kt`);
    if (pos.course != null) meta.push(`${pos.course}\u00B0`);
    if (pos.has_alt) meta.push(`${Math.round(pos.alt_m)}m`);
    if (meta.length) html += `<div class="stn-meta">${meta.join(' \u00B7 ')}</div>`;
    // Via
    html += `<div class="stn-via ${_viaCls(s)}">${_viaText(s)}</div>`;
    // Path (only when hops > 0)
    if (s.hops > 0 && s.path && s.path.length) {
      html += `<div class="stn-path">${s.path.map(_esc).join(',')}</div>`;
    }
    // Comment
    if (s.comment) {
      html += `<div class="stn-sep"></div>`;
      html += `<div class="stn-comment">${_esc(s.comment)}</div>`;
    }
    html += `</div>`;
    return html;
  }
}

// --- helpers ---

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

function _fmtLat(lat) {
  const dir = lat >= 0 ? 'N' : 'S';
  return `${Math.abs(lat).toFixed(4)}\u00B0${dir}`;
}

function _fmtLon(lon) {
  const dir = lon >= 0 ? 'E' : 'W';
  return `${Math.abs(lon).toFixed(4)}\u00B0${dir}`;
}

function _viaCls(s) {
  if (s.via === 'is') return 'via-is';
  if (s.hops > 0) return 'via-rf-hops';
  return 'via-rf';
}

function _viaText(s) {
  if (s.via === 'is') return 'APRS-IS';
  if (s.hops > 0) return `RF via ${s.hops} hop${s.hops > 1 ? 's' : ''}`;
  return 'RF direct';
}
