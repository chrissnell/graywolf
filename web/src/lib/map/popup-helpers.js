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
