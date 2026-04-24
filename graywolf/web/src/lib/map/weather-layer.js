// Weather overlay for stations reporting weather data.
//
// Renders temperature and wind info as text labels near weather stations.
// Only active when the weather layer toggle is enabled and the server
// response includes weather data (via include=weather query param).

import L from 'leaflet';
import { unitsState } from '../settings/units-store.svelte.js';

const KMH_PER_MPH = 1.60934;

export class WeatherLayer {
  constructor(map) {
    this.map = map;
    this.layerGroup = L.layerGroup();
    this._visible = false;
  }

  // Render weather labels for stations with weather data.
  // stations: array of station objects from the server (must have weather field).
  update(stations) {
    this.layerGroup.clearLayers();

    for (const s of stations) {
      if (!s.weather || !s.positions || !s.positions.length) continue;
      const pos = s.positions[0];
      const label = this._formatLabel(s.weather);
      if (!label) continue;

      L.marker([pos.lat, pos.lon], {
        icon: L.divIcon({
          className: 'wx-label',
          html: `<div class="wx-text">${label}</div>`,
          iconSize: [80, 24],
          iconAnchor: [40, -14], // above the station icon
        }),
        interactive: false,
      }).addTo(this.layerGroup);
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

  _formatLabel(wx) {
    const metric = unitsState.isMetric;
    const parts = [];
    if (wx.temp_f != null) {
      const t = metric ? (wx.temp_f - 32) * 5 / 9 : wx.temp_f;
      parts.push(`${Math.round(t)}°${metric ? 'C' : 'F'}`);
    }
    if (wx.wind_mph != null) {
      const s = metric ? wx.wind_mph * KMH_PER_MPH : wx.wind_mph;
      let wind = `${Math.round(s)}${metric ? 'km/h' : 'mph'}`;
      if (wx.wind_dir != null) wind = `${wind} ${_cardinal(wx.wind_dir)}`;
      parts.push(wind);
    }
    return parts.join(' ');
  }
}

function _cardinal(deg) {
  const dirs = ['N', 'NE', 'E', 'SE', 'S', 'SW', 'W', 'NW'];
  return dirs[Math.round(deg / 45) % 8];
}
