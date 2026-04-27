// Station popup HTML factory. The CSS classes (.stn-popup, .stn-hdr,
// .stn-call, .stn-sub, .stn-coords, .stn-meta, .stn-via, .stn-path,
// .stn-comment, .badge, .b-rx, .b-tx, .b-is, .via-is, .via-rf,
// .via-rf-hops, .path-link) are defined :global() in LiveMapV2.svelte.

import { esc, timeAgo, fmtLat, fmtLon, viaCls, viaText } from './popup-helpers.js';

// renderStationPopupHTML(station, { hasStation }) -> HTML string
//
// hasStation(callsign) is an optional predicate used to decide whether a
// digipeater entry in the path field renders as a clickable .path-link
// or plain text. Pass null to render every entry as plain text.
export function renderStationPopupHTML(s, { hasStation = null } = {}) {
  const pos = s.positions && s.positions[0];
  if (!pos) return '';

  const ago = timeAgo(s.last_heard);
  const dirCls =
    s.direction === 'RX' ? 'b-rx' : s.direction === 'TX' ? 'b-tx' : 'b-is';

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
  if (pos.course != null) meta.push(`${pos.course}°`);
  if (pos.has_alt) meta.push(`alt ${Math.round(pos.alt_m * 3.28084)} ft`);
  if (meta.length) html += `<div class="stn-meta">${meta.join(' · ')}</div>`;

  html += `<div class="stn-via ${viaCls(s)}">${viaText(s)}</div>`;

  if (s.hops > 0 && s.path && s.path.length) {
    const pathHtml = s.path
      .map((call) => {
        const clean = call.replace('*', '');
        const suffix = call.endsWith('*') ? '*' : '';
        if (hasStation && hasStation(clean)) {
          return `<a class="path-link" href="#" data-callsign="${esc(clean)}">${esc(clean)}${suffix}</a>`;
        }
        return esc(call);
      })
      .join(',');
    html += `<div class="stn-path">${pathHtml}</div>`;
  }

  if (s.comment) {
    html += `<div class="stn-sep"></div>`;
    html += `<div class="stn-comment">${esc(s.comment)}</div>`;
  }
  html += `</div>`;
  return html;
}
