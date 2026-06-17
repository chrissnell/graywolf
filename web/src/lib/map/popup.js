// Station popup HTML factory. The CSS classes (.stn-popup, .stn-hdr,
// .stn-call, .stn-sub, .stn-src, .stn-src-icon, .stn-src-from,
// .stn-src-call, .stn-coords, .stn-meta, .stn-via, .stn-path,
// .stn-comment, .badge, .b-rx, .b-tx, .b-is, .via-is, .via-rf,
// .via-rf-hops, .path-link) are defined :global() in LiveMapV2.svelte.

import { esc, timeAgo, fmtLat, fmtLon, viaCls, viaText, formatWeatherRows } from './popup-helpers.js';
import { unitsState } from '../settings/units-store.svelte.js';

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

  // For an object/item, the header is the object NAME, not a station — so
  // surface the originating station (the AX.25 source that created and
  // transmitted it) right beneath the title. Without this the popup only
  // shows the relay path below, making an object look like it came from
  // whoever digipeated it rather than its true author (GH #323). Rendered
  // as a clickable path-link when that station is itself on the map.
  if (s.is_object && s.source) {
    html += renderObjectSourceHTML(s.source, hasStation);
  }

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

  const wxRows = formatWeatherRows(s.weather, unitsState.isMetric);
  if (wxRows.length) {
    html += `<div class="stn-sep"></div>`;
    html += `<div class="stn-weather">`;
    for (const [label, val] of wxRows) {
      html += `<div class="stn-weather-row"><span class="stn-weather-label">${esc(label)}</span><span class="stn-weather-val">${esc(val)}</span></div>`;
    }
    html += `</div>`;
  }

  if (s.comment) {
    html += `<div class="stn-sep"></div>`;
    html += `<div class="stn-comment">${esc(s.comment)}</div>`;
  }

  const actions = renderStationActionsHTML(s);
  if (actions) {
    html += `<div class="stn-sep"></div>`;
    html += actions;
  }

  html += `</div>`;
  return html;
}

// Inline lucide-style icon. Mirrors the markup lucide-svelte emits so the
// action rows visually match the map right-click menu (which uses the same
// icons via lucide-svelte). 14px / strokeWidth 2 to match .menu-icon.
function icon(body, cls = 'stn-action-icon') {
  return (
    `<svg class="${cls}" xmlns="http://www.w3.org/2000/svg" ` +
    `width="14" height="14" viewBox="0 0 24 24" fill="none" ` +
    `stroke="currentColor" stroke-width="2" stroke-linecap="round" ` +
    `stroke-linejoin="round" aria-hidden="true">${body}</svg>`
  );
}

// lucide "radio" (broadcast) — marks the station that transmitted the object.
const ICON_SOURCE = icon(
  '<path d="M4.9 19.1C1 15.2 1 8.8 4.9 4.9"/>' +
    '<path d="M7.8 16.2c-2.3-2.3-2.3-6.1 0-8.5"/>' +
    '<circle cx="12" cy="12" r="2"/>' +
    '<path d="M16.2 7.8c2.3 2.3 2.3 6.1 0 8.5"/>' +
    '<path d="M19.1 4.9C23 8.8 23 15.1 19.1 19"/>',
  'stn-src-icon'
);

// renderObjectSourceHTML(source, hasStation) -> HTML string
//
// The "from CALLSIGN" line shown under an object/item title. When the
// originating station is itself on the map, the callsign is a path-link
// (the popup's click handler pans to it and reopens its popup); otherwise
// it's plain emphasized text.
function renderObjectSourceHTML(source, hasStation) {
  const call =
    hasStation && hasStation(source)
      ? `<a class="path-link stn-src-call" href="#" data-callsign="${esc(source)}">${esc(source)}</a>`
      : `<span class="stn-src-call">${esc(source)}</span>`;
  return `<div class="stn-src">${ICON_SOURCE}<span class="stn-src-from">from</span>${call}</div>`;
}

const ICON_MESSAGE = icon(
  '<path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"/>'
);
const ICON_LOGS = icon(
  '<path d="M15 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7Z"/>' +
    '<path d="M14 2v4a2 2 0 0 0 2 2h4"/><path d="M10 9H8"/>' +
    '<path d="M16 13H8"/><path d="M16 17H8"/>'
);
const ICON_QRZ = icon(
  '<path d="M15 3h6v6"/><path d="M10 14 21 3"/>' +
    '<path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"/>'
);

// renderStationActionsHTML(station) -> HTML string (or '' to suppress)
//
// Action rows shown for a real heard station: open a direct message thread,
// view the APRS packet log filtered to this callsign, and a QRZ database
// lookup. APRS objects/items aren't operators you can work, so they get no
// actions. Messages and Logs are internal hash routes; QRZ is the one
// external link (opens in a new tab). Styled to match the map right-click
// context menu -- icon + label rows with a hover tint (see .stn-action in
// LiveMapV2.svelte).
export function renderStationActionsHTML(s) {
  const call = s.callsign;
  if (!call || s.is_object) return '';

  const upper = call.toUpperCase();
  // QRZ indexes operators by base callsign, not by APRS SSID, so strip any
  // "-N" suffix (e.g. W1ABC-9 -> W1ABC) before building the lookup URL.
  const qrzCall = upper.split('-')[0];
  const qrzHref = `https://www.qrz.com/db/${encodeURIComponent(qrzCall)}`;
  const msgHref = `#/messages?thread=${encodeURIComponent('dm:' + upper)}`;
  const logHref = `#/logs?callsign=${encodeURIComponent(upper)}`;

  let html = `<div class="stn-actions" role="menu">`;
  html += `<a class="stn-action stn-msg-link" role="menuitem" href="${msgHref}">${ICON_MESSAGE}<span class="stn-action-label">Message</span></a>`;
  html += `<a class="stn-action stn-log-link" role="menuitem" href="${logHref}">${ICON_LOGS}<span class="stn-action-label">APRS logs</span></a>`;
  html += `<a class="stn-action stn-qrz-link" role="menuitem" href="${qrzHref}" target="_blank" rel="noopener noreferrer">${ICON_QRZ}<span class="stn-action-label">QRZ</span></a>`;
  html += `</div>`;
  return html;
}
