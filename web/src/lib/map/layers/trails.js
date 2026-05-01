// Trails: per-callsign colored polylines + circle dots at historical
// position fixes, mirroring the legacy Leaflet TrailLayer styling.
//
// - 7-color deterministic palette keyed off callsign hash.
// - Per-segment fading opacity: newest segment ~0.9, oldest ~0.3.
// - White-filled colored dots at every previous fix (positions[1..]).
//   positions[0] is intentionally NOT dotted -- the station icon already
//   sits there.
// - Click a dot for a popup carrying that fix's packet metadata
//   (timestamp, coords, speed/course/alt, via, path, comment).
// - Hover a line or dot for a small callsign tooltip.
//
// Implementation:
//   * One LineString feature per segment with `color` + `opacity`
//     properties read by paint expressions, so a single line layer
//     handles every callsign.
//   * Two circle layers backed by a shared point source: an invisible
//     fat hit zone (radius 12) so dots are easy to click on touch, and
//     the visible 3px dot on top.
//   * Path arrays are JSON-stringified into the feature properties so
//     they survive the GeoJSON round-trip; we parse them back on click.
//
// MapLibre canvas layers always render below DOM-based markers, so the
// trail line and dots naturally sit beneath the station icons without
// explicit beforeLayerId placement.

import maplibregl from 'maplibre-gl';
import { esc, timeAgo, fmtLat, fmtLon, viaCls, viaText } from '../popup-helpers.js';

const LINE_SOURCE_ID = 'gw-trails-lines';
const DOT_SOURCE_ID = 'gw-trails-dots';
const LINE_LAYER_ID = 'gw-trails-line';
const DOT_HIT_LAYER_ID = 'gw-trails-dot-hit';
const DOT_LAYER_ID = 'gw-trails-dot';

// Dark saturated palette that pops on the basemap. Order kept stable so
// a given callsign always maps to the same color across sessions.
const TRAIL_COLORS = [
  '#2b6cb0', // dark blue
  '#6b21a8', // dark purple
  '#4a9e3f', // lime green
  '#b5247a', // hot pink
  '#1a8a9a', // teal
  '#b45309', // dark amber
  '#be123c', // crimson
];

function trailColor(callsign) {
  let h = 0;
  for (let i = 0; i < callsign.length; i++) {
    h = (h * 31 + callsign.charCodeAt(i)) | 0;
  }
  const idx = ((h % TRAIL_COLORS.length) + TRAIL_COLORS.length) % TRAIL_COLORS.length;
  return TRAIL_COLORS[idx];
}

export function mountTrailsLayer(map, getStations, opts = {}) {
  const { hasStation = () => false, focusStation = () => {} } = opts;

  if (!map.getSource(LINE_SOURCE_ID)) {
    map.addSource(LINE_SOURCE_ID, {
      type: 'geojson',
      data: { type: 'FeatureCollection', features: [] },
    });
  }
  if (!map.getSource(DOT_SOURCE_ID)) {
    map.addSource(DOT_SOURCE_ID, {
      type: 'geojson',
      data: { type: 'FeatureCollection', features: [] },
    });
  }
  if (!map.getLayer(LINE_LAYER_ID)) {
    map.addLayer({
      id: LINE_LAYER_ID,
      type: 'line',
      source: LINE_SOURCE_ID,
      layout: { 'line-cap': 'round', 'line-join': 'round' },
      paint: {
        'line-color': ['get', 'color'],
        'line-width': 4,
        'line-opacity': ['get', 'opacity'],
      },
    });
  }
  if (!map.getLayer(DOT_HIT_LAYER_ID)) {
    map.addLayer({
      id: DOT_HIT_LAYER_ID,
      type: 'circle',
      source: DOT_SOURCE_ID,
      paint: {
        'circle-radius': 12,
        'circle-color': '#000',
        'circle-opacity': 0,
      },
    });
  }
  if (!map.getLayer(DOT_LAYER_ID)) {
    map.addLayer({
      id: DOT_LAYER_ID,
      type: 'circle',
      source: DOT_SOURCE_ID,
      paint: {
        'circle-radius': 3,
        'circle-color': '#ffffff',
        'circle-stroke-color': ['get', 'color'],
        'circle-stroke-width': 2,
        'circle-opacity': ['get', 'opacity'],
        'circle-stroke-opacity': ['get', 'opacity'],
      },
    });
  }

  let popup = null;
  let hoverPopup = null;

  function clearHoverTooltip() {
    if (hoverPopup) { hoverPopup.remove(); hoverPopup = null; }
  }

  function showHoverTooltip(callsign, lngLat) {
    if (hoverPopup) {
      hoverPopup.setLngLat(lngLat).setText(callsign);
      return;
    }
    hoverPopup = new maplibregl.Popup({
      offset: 8,
      closeButton: false,
      closeOnClick: false,
      className: 'gw-trail-tooltip',
    })
      .setLngLat(lngLat)
      .setText(callsign)
      .addTo(map);
  }

  function openDotPopup(props, lngLat) {
    if (popup) popup.remove();
    const html = renderDotPopup(props, hasStation);
    popup = new maplibregl.Popup({
      offset: 12,
      closeButton: true,
      closeOnClick: true,
      maxWidth: '320px',
      className: 'gw-station-popup',
    })
      .setLngLat(lngLat)
      .setHTML(html)
      .addTo(map);
    popup.on('close', () => { popup = null; });

    // Wire path-link clicks so clicking a digi callsign in the path
    // re-focuses the map on it.
    const el = popup.getElement();
    if (el) {
      el.addEventListener('click', (ev) => {
        const link = ev.target && ev.target.closest && ev.target.closest('.path-link');
        if (!link) return;
        ev.preventDefault();
        const cs = link.dataset.callsign;
        if (cs) focusStation(cs);
      });
    }
  }

  function onDotClick(e) {
    const f = e.features && e.features[0];
    if (!f) return;
    openDotPopup(f.properties, f.geometry.coordinates);
  }

  function onHoverEnter(e) {
    const f = e.features && e.features[0];
    if (!f) return;
    map.getCanvas().style.cursor = 'pointer';
    showHoverTooltip(f.properties.callsign, e.lngLat);
  }

  function onHoverMove(e) {
    const f = e.features && e.features[0];
    if (!f) { clearHoverTooltip(); return; }
    showHoverTooltip(f.properties.callsign, e.lngLat);
  }

  function onHoverLeave() {
    map.getCanvas().style.cursor = '';
    clearHoverTooltip();
  }

  map.on('click', DOT_LAYER_ID, onDotClick);
  map.on('click', DOT_HIT_LAYER_ID, onDotClick);
  map.on('mouseenter', DOT_HIT_LAYER_ID, onHoverEnter);
  map.on('mousemove', DOT_HIT_LAYER_ID, onHoverMove);
  map.on('mouseleave', DOT_HIT_LAYER_ID, onHoverLeave);
  map.on('mouseenter', LINE_LAYER_ID, onHoverEnter);
  map.on('mousemove', LINE_LAYER_ID, onHoverMove);
  map.on('mouseleave', LINE_LAYER_ID, onHoverLeave);

  // Optional per-station predicate. Stations failing it are skipped
  // entirely (no line, no dots). Driven by the Direct RX toggle.
  let filter = null;

  function refresh() {
    const stations = getStations();
    if (!stations) return;

    const lineFeatures = [];
    const dotFeatures = [];

    for (const [callsign, s] of stations) {
      if (filter && !filter(s)) continue;
      const pts = s.positions;
      if (!pts || pts.length < 2) continue;
      const color = trailColor(callsign);
      const segCount = pts.length - 1;

      for (let i = 0; i < segCount; i++) {
        const opacity = Math.max(0.9 - (i / segCount) * 0.6, 0.3);
        lineFeatures.push({
          type: 'Feature',
          geometry: {
            type: 'LineString',
            coordinates: [
              [pts[i].lon, pts[i].lat],
              [pts[i + 1].lon, pts[i + 1].lat],
            ],
          },
          properties: { callsign, color, opacity },
        });
      }

      for (let i = 1; i < pts.length; i++) {
        const p = pts[i];
        const opacity = Math.max(0.9 - ((i - 1) / segCount) * 0.5, 0.4);
        dotFeatures.push({
          type: 'Feature',
          geometry: { type: 'Point', coordinates: [p.lon, p.lat] },
          properties: {
            callsign,
            color,
            opacity,
            timestamp: p.timestamp,
            lat: p.lat,
            lon: p.lon,
            alt_m: p.alt_m == null ? null : p.alt_m,
            has_alt: !!p.has_alt,
            speed_kt: p.speed_kt || 0,
            course: p.course == null ? null : p.course,
            channel: p.channel || 0,
            via: p.via || '',
            hops: p.hops || 0,
            direction: p.direction || 'RX',
            comment: p.comment || '',
            // Stringify so the array survives any GeoJSON round-trip;
            // parsed back in renderDotPopup.
            path: JSON.stringify(p.path || []),
          },
        });
      }
    }

    map.getSource(LINE_SOURCE_ID)?.setData({ type: 'FeatureCollection', features: lineFeatures });
    map.getSource(DOT_SOURCE_ID)?.setData({ type: 'FeatureCollection', features: dotFeatures });
  }

  function setVisible(visible) {
    const value = visible ? 'visible' : 'none';
    for (const id of [LINE_LAYER_ID, DOT_HIT_LAYER_ID, DOT_LAYER_ID]) {
      if (map.getLayer(id)) map.setLayoutProperty(id, 'visibility', value);
    }
    if (!visible) {
      if (popup) { popup.remove(); popup = null; }
      clearHoverTooltip();
    }
  }

  function destroy() {
    // Parent route's onDestroy runs after the map shell's, which means
    // map.remove() may already have torn down internal state. Guard --
    // if the map is gone, the layer/source went with it.
    try {
      map.off('click', DOT_LAYER_ID, onDotClick);
      map.off('click', DOT_HIT_LAYER_ID, onDotClick);
      map.off('mouseenter', DOT_HIT_LAYER_ID, onHoverEnter);
      map.off('mousemove', DOT_HIT_LAYER_ID, onHoverMove);
      map.off('mouseleave', DOT_HIT_LAYER_ID, onHoverLeave);
      map.off('mouseenter', LINE_LAYER_ID, onHoverEnter);
      map.off('mousemove', LINE_LAYER_ID, onHoverMove);
      map.off('mouseleave', LINE_LAYER_ID, onHoverLeave);
      if (popup) { popup.remove(); popup = null; }
      clearHoverTooltip();
      for (const id of [DOT_LAYER_ID, DOT_HIT_LAYER_ID, LINE_LAYER_ID]) {
        if (map.getLayer(id)) map.removeLayer(id);
      }
      for (const id of [DOT_SOURCE_ID, LINE_SOURCE_ID]) {
        if (map.getSource(id)) map.removeSource(id);
      }
    } catch { /* map already removed */ }
  }

  function setFilter(pred) {
    filter = typeof pred === 'function' ? pred : null;
    refresh();
  }

  return { refresh, destroy, setVisible, setFilter };
}

// Trail-dot popup uses per-position metadata so it reflects the packet
// state at the time this fix was reported, not the station's current
// state. Mirrors the legacy Leaflet trail dot popup.
function renderDotPopup(props, hasStation) {
  const ago = timeAgo(props.timestamp);
  const dir = props.direction || 'RX';
  const dirCls = dir === 'RX' ? 'b-rx' : dir === 'TX' ? 'b-tx' : 'b-is';

  let html = `<div class="stn-popup">`;
  html += `<div class="stn-hdr">`;
  html += `<span class="stn-call">${esc(props.callsign)}</span>`;
  if (dir !== 'IS') {
    html += `<span class="badge ${dirCls}">${esc(dir)}</span>`;
  }
  html += `</div>`;
  html += `<div class="stn-sub">${ago}`;
  if (props.channel) html += ` &middot; Ch ${props.channel}`;
  html += `</div>`;
  html += `<div class="stn-sep"></div>`;
  html += `<div class="stn-coords">${fmtLat(Number(props.lat))} ${fmtLon(Number(props.lon))}</div>`;

  const meta = [];
  const speedKt = Number(props.speed_kt) || 0;
  if (speedKt > 0) meta.push(`${Math.round(speedKt * 1.15078)}mph`);
  if (props.course !== null && props.course !== '' && props.course !== undefined) {
    meta.push(`${props.course}°`);
  }
  if (props.has_alt) {
    const altM = Number(props.alt_m);
    if (!Number.isNaN(altM)) meta.push(`alt ${Math.round(altM * 3.28084)} ft`);
  }
  if (meta.length) html += `<div class="stn-meta">${meta.join(' · ')}</div>`;

  // viaCls/viaText want { via, hops } -- the per-fix props already have
  // these flat, so just pass props directly.
  const viaShim = { via: props.via, hops: Number(props.hops) || 0 };
  html += `<div class="stn-via ${viaCls(viaShim)}">${viaText(viaShim)}</div>`;

  let path = [];
  try { path = JSON.parse(props.path || '[]'); } catch { path = []; }
  if ((Number(props.hops) || 0) > 0 && path.length) {
    const pathHtml = path.map((call) => {
      const clean = call.replace('*', '');
      const suffix = call.endsWith('*') ? '*' : '';
      if (hasStation(clean)) {
        return `<a class="path-link" href="#" data-callsign="${esc(clean)}">${esc(clean)}${suffix}</a>`;
      }
      return esc(call);
    }).join(',');
    html += `<div class="stn-path">${pathHtml}</div>`;
  }

  if (props.comment) {
    html += `<div class="stn-sep"></div>`;
    html += `<div class="stn-comment">${esc(props.comment)}</div>`;
  }

  html += `</div>`;
  return html;
}
