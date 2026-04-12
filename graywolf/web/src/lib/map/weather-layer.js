// Weather overlay for stations reporting weather data.
//
// Renders temperature and wind info as text labels near weather stations.
// Only active when the weather layer toggle is enabled and the server
// response includes weather data (via include=weather query param).

import L from 'leaflet';

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
    const parts = [];
    if (wx.temp_f != null) parts.push(`${Math.round(wx.temp_f)}\u00B0F`);
    if (wx.wind_mph != null) {
      let wind = `${Math.round(wx.wind_mph)}mph`;
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
