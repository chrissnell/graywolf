// Station popup HTML factory. The CSS classes (.stn-popup, .stn-hdr,
// .stn-call, .stn-sub, .stn-device, .stn-src, .stn-src-icon, .stn-src-from,
// .stn-src-call, .stn-coords, .stn-meta, .stn-via, .stn-path,
// .stn-comment, .badge, .b-rx, .b-tx, .b-is, .via-is, .via-rf,
// .via-rf-hops, .path-link) are defined :global() in LiveMapV2.svelte.

import { esc, timeAgo, fmtLat, fmtLon, viaCls, viaText, formatWeatherRows, deviceText } from './popup-helpers.js';
import { rfReachableDespiteNonRfLatest } from './rf-only-core.js';
import { unitsState } from '../settings/units-store.svelte.js';

// renderStationPopupHTML(station, { hasStation, isFavorite }) -> HTML string
//
// hasStation(callsign) is an optional predicate used to decide whether a
// digipeater entry in the path field renders as a clickable .path-link
// or plain text. Pass null to render every entry as plain text.
// isFavorite is whether this callsign is currently on the operator's
// favorites list (favoriteStationsStore) -- drives the star action's
// filled/outline state.
// isExcluded is whether this callsign is currently on the operator's
// excluded-stations list (excludedStationsStore) -- drives the
// exclude action's "Exclude"/"Excluded" label and pressed state.
export function renderStationPopupHTML(s, { hasStation = null, isFavorite = false, isExcluded = false } = {}) {
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
  // Mic-E status (APRS101 ch 10 table 8) or '>' status report text.
  // "Off Duty" is the routine default and not worth a badge; status_code
  // 0 is Emergency -- the one status that also raises a popup/OS/sound
  // notification (stationAlertsTransport.js) -- so it gets the alarming
  // b-emergency style. Everything else (Priority, Special, Committed,
  // Returning, En Route, or a free-form status string) is informational.
  if (s.status_text && s.status_text !== 'Off Duty') {
    const statusCls = s.status_code === 0 ? 'b-emergency' : 'b-status';
    html += `<span class="badge ${statusCls}">${esc(s.status_text)}</span>`;
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

  // Device identification (manufacturer/model/class) inferred server-side
  // from the packet's TOCALL, or the Mic-E manufacturer byte as a
  // fallback -- e.g. "Yaesu: FT5D (ht)". Omitted when the tocall pattern
  // is unrecognized. Not looked up via aprs.fi or any external service.
  if (s.device) {
    const dtext = deviceText(s.device);
    if (dtext) html += `<div class="stn-device">Device: ${esc(dtext)}</div>`;
  }

  html += `<div class="stn-sep"></div>`;
  html += `<div class="stn-coords">${fmtLat(pos.lat)} ${fmtLon(pos.lon)}</div>`;

  const meta = [];
  if (pos.speed_kt > 0) meta.push(`${Math.round(pos.speed_kt * 1.15078)}mph`);
  if (pos.course != null) meta.push(`${pos.course}°`);
  if (pos.has_alt) meta.push(`alt ${Math.round(pos.alt_m * 3.28084)} ft`);
  if (meta.length) html += `<div class="stn-meta">${meta.join(' · ')}</div>`;

  html += `<div class="stn-via ${viaCls(s)}">${viaText(s)}</div>`;

  // RF-reachability note. The badge and via line above reflect the *latest*
  // packet (station-level fields, overwritten unconditionally by every
  // arrival), but the marker is plotted at positions[0], whose reception the
  // server pins to the most RF-reachable copy of that fix (stationcache
  // rfRank). When the latest packet arrived via APRS-IS (or Internet-to-RF
  // gated) yet the plotted fix was heard on RF, the station still -- correctly
  // -- qualifies for the "RF Only" filter. Surface that here so the "APRS-IS" via
  // doesn't read as a filter bug (graywolf #482, the second report of this
  // divergence after #394).
  if (rfReachableDespiteNonRfLatest(s)) {
    const heard = pos.hops > 0
      ? `via ${pos.hops} hop${pos.hops > 1 ? 's' : ''}`
      : 'direct';
    html +=
      `<div class="stn-rf-reachable" title="This station's plotted position was heard on RF (radio), so it stays visible under the RF Only filter even though its most recent packet did not arrive over RF (APRS-IS or Internet-to-RF gated).">` +
      `RF-reachable &middot; plotted fix heard on RF (${heard})</div>`;
  }

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

  const actions = renderStationActionsHTML(s, { isFavorite, isExcluded });
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

// lucide "star" — the favorites toggle. filled=true renders a solid star
// (currently a favorite) instead of an outline, mirrors how a filled vs
// outline heart/star icon reads universally as "already saved" vs "save".
function iconStar(filled) {
  return (
    `<svg class="stn-action-icon" xmlns="http://www.w3.org/2000/svg" ` +
    `width="14" height="14" viewBox="0 0 24 24" fill="${filled ? 'currentColor' : 'none'}" ` +
    `stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" ` +
    `aria-hidden="true"><polygon points="12 2 15.09 8.26 22 9.27 17 14.14 18.18 21.02 12 17.77 5.82 21.02 7 14.14 2 9.27 8.91 8.26 12 2"/></svg>`
  );
}

// lucide "bell-off" — the exclude-from-notifications toggle. Excluding a
// station on excludedStationsStore stops it from ever raising a
// new-station or favorite notification (stationNewTransport.js checks
// excludedStationsStore.has() before either notification path), so the
// bell-with-a-slash reads as "notifications off for this station".
const ICON_EXCLUDE = icon(
  '<path d="M8.7 3A6 6 0 0 1 18 8a21.3 21.3 0 0 0 .6 5"/>' +
    '<path d="M17 17H3s3-2 3-9a4.67 4.67 0 0 1 .3-1.7"/>' +
    '<path d="M10.3 21a1.94 1.94 0 0 0 3.4 0"/>' +
    '<path d="m2 2 20 20"/>'
);

// renderStationActionsHTML(station, { isFavorite, isExcluded }) -> HTML string (or '' to suppress)
//
// Action rows shown for a real heard station: open a direct message thread,
// view the APRS packet log filtered to this callsign, a QRZ database
// lookup, a favorites star toggle, and an exclude-from-notifications
// toggle. APRS objects/items aren't operators you can work, so they get no
// actions. Messages and Logs are internal hash routes; QRZ is the one
// external link (opens in a new tab); the star and bell-off buttons are
// <button>s (not links -- they mutate state via favoriteStationsStore /
// excludedStationsStore rather than navigating), wired up by
// LiveMapV2.svelte's popup click delegation the same way .path-link
// clicks are. Styled to match the map right-click context menu -- icon +
// label rows with a hover tint (see .stn-action in LiveMapV2.svelte).
export function renderStationActionsHTML(s, { isFavorite = false, isExcluded = false } = {}) {
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
  html +=
    `<button type="button" class="stn-action stn-fav-btn${isFavorite ? ' is-favorite' : ''}" role="menuitem" ` +
    `data-callsign="${esc(upper)}" aria-pressed="${isFavorite}">${iconStar(isFavorite)}` +
    `<span class="stn-action-label">${isFavorite ? 'Favorited' : 'Favorite'}</span></button>`;
  html +=
    `<button type="button" class="stn-action stn-exclude-btn${isExcluded ? ' is-excluded' : ''}" role="menuitem" ` +
    `data-callsign="${esc(upper)}" aria-pressed="${isExcluded}">${ICON_EXCLUDE}` +
    `<span class="stn-action-label">${isExcluded ? 'Excluded' : 'Exclude'}</span></button>`;
  html += `</div>`;
  return html;
}
