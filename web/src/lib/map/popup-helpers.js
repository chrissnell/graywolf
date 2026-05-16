// Shared helpers for station and trail popup rendering.

export function esc(str) {
  if (!str) return '';
  return str.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
}

export function timeAgo(isoStr) {
  const ms = Date.now() - new Date(isoStr).getTime();
  const sec = Math.floor(ms / 1000);
  if (sec < 60) return `${sec}s ago`;
  const min = Math.floor(sec / 60);
  if (min < 60) return `${min} min ago`;
  const hr = Math.floor(min / 60);
  return `${hr}h ${min % 60}m ago`;
}

export function fmtLat(lat) {
  const dir = lat >= 0 ? 'N' : 'S';
  return `${Math.abs(lat).toFixed(4)}\u00B0${dir}`;
}

export function fmtLon(lon) {
  const dir = lon >= 0 ? 'E' : 'W';
  return `${Math.abs(lon).toFixed(4)}\u00B0${dir}`;
}

export function viaCls(s) {
  if (s.via === 'is') return 'via-is';
  if (s.hops > 0) return 'via-rf-hops';
  return 'via-rf';
}

export function viaText(s) {
  if (s.via === 'is') return 'APRS-IS';
  if (s.hops > 0) return `RF via ${s.hops} hop${s.hops > 1 ? 's' : ''}`;
  return 'RF direct';
}

export const KMH_PER_MPH = 1.60934;
const MM_PER_IN = 25.4;

export function cardinal(deg) {
  const dirs = ['N', 'NE', 'E', 'SE', 'S', 'SW', 'W', 'NW'];
  return dirs[Math.round(deg / 45) % 8];
}

// formatWeatherRows turns a WeatherDTO into [label, value] pairs the
// popup renders as a label/value list. Honors the metric toggle for
// temperature, wind, and rain/snow; pressure stays in mb (used by both
// systems on weather displays); luminosity stays in W/m². Rain/snow
// fields arrive in inches (converted server-side in stationcache).
export function formatWeatherRows(wx, isMetric) {
  if (!wx) return [];
  const rows = [];
  if (wx.temp_f != null) {
    const t = isMetric ? ((wx.temp_f - 32) * 5) / 9 : wx.temp_f;
    rows.push(['Temp', `${Math.round(t)}°${isMetric ? 'C' : 'F'}`]);
  }
  if (wx.wind_mph != null) {
    const s = isMetric ? wx.wind_mph * KMH_PER_MPH : wx.wind_mph;
    let v = `${Math.round(s)} ${isMetric ? 'km/h' : 'mph'}`;
    if (wx.wind_dir != null) v += ` ${cardinal(wx.wind_dir)}`;
    rows.push(['Wind', v]);
  }
  if (wx.gust_mph != null) {
    const g = isMetric ? wx.gust_mph * KMH_PER_MPH : wx.gust_mph;
    rows.push(['Gust', `${Math.round(g)} ${isMetric ? 'km/h' : 'mph'}`]);
  }
  if (wx.humidity != null) {
    rows.push(['Humidity', `${wx.humidity}%`]);
  }
  if (wx.pressure_mb != null) {
    rows.push(['Pressure', `${wx.pressure_mb.toFixed(1)} mb`]);
  }
  if (wx.rain_1h_in != null) {
    rows.push([
      'Rain 1h',
      isMetric
        ? `${(wx.rain_1h_in * MM_PER_IN).toFixed(1)} mm`
        : `${wx.rain_1h_in.toFixed(2)}″`,
    ]);
  }
  if (wx.rain_24h_in != null) {
    rows.push([
      'Rain 24h',
      isMetric
        ? `${(wx.rain_24h_in * MM_PER_IN).toFixed(1)} mm`
        : `${wx.rain_24h_in.toFixed(2)}″`,
    ]);
  }
  if (wx.snow_24h_in != null) {
    rows.push([
      'Snow 24h',
      isMetric
        ? `${(wx.snow_24h_in * MM_PER_IN).toFixed(1)} mm`
        : `${wx.snow_24h_in.toFixed(2)}″`,
    ]);
  }
  if (wx.luminosity_wm2 != null) {
    rows.push(['Solar', `${wx.luminosity_wm2} W/m²`]);
  }
  return rows;
}
