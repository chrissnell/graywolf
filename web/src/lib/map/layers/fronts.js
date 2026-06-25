// Surface-fronts overlay layer for the Live Map (WMO frontal symbology).
//
// Backend-agnostic in the same spirit as radar.js: it asks fronts-source.js for
// provider descriptors and performs the MapLibre source/layer calls. Unlike
// radar there is no per-frame loop -- a single GeoJSON document per source holds
// the current analysis (fronts + pressure centers), and the overlay renders
// whatever features it carries. A slow manifest poll (driven by LiveMapV2)
// pushes a fresh document via setData()/setWorldData() when a new analysis is
// published.
//
// TWO sources, one toggle:
//   fronts       -- WPC coded surface bulletin (analysis, North America)
//   fronts-world -- model-derived global fronts (GFS Thermal Front Parameter)
// Both render with identical styling (same paint/layout, same pip sprites) and
// both run through the same Catmull-Rom smoothing before reaching MapLibre. The
// world layers are inserted BENEATH the WPC layers so the analyst product wins
// over North America and the model shows through everywhere else. setVisible /
// refresh / destroy fan out to both.
//
// Frontal pips are sprite icons placed along the line (symbol-placement:line).
// One colored sprite is baked per front type at registration time (the fill is
// parameterized with the front-type color, then rasterized as a normal non-SDF
// image). Earlier versions registered a single black silhouette as an SDF image
// tinted at runtime via icon-color, but MapLibre's sdf flag reads the alpha
// channel as a signed distance field -- a hard-rasterized binary mask is not a
// distance field, so tinting fringed the edges at interpolated icon-size.

import {
  frontsProvider,
  frontsWorldProvider,
  FRONTS_SOURCE_ID,
  FRONTS_WORLD_SOURCE_ID,
  FRONT_COLORS,
} from '../sources/fronts-source.js';

// Pip glyph markup. Kept inline (not a Vite `?raw` import) so this module loads
// unchanged under plain `node --test`, which has no Vite to resolve `?raw`. The
// canonical, hand-editable copies live alongside as SVG files -- keep these in
// sync with them:
//   ../style/front-sprites/cold.svg       (cold triangle, base on baseline,
//                                           points up)
//   ../style/front-sprites/warm.svg       (warm semicircle, flat edge on
//                                           baseline)
//   ../style/front-sprites/occluded-tri.svg  (same triangle, used for occluded)
const coldSvg = (fill) =>
  `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 18 18" width="18" height="18"><polygon points="2,9 16,9 9,1" fill="${fill}"/></svg>`;
const warmSvg = (fill) =>
  `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 18 18" width="18" height="18"><path d="M 2 9 A 7 7 0 0 1 16 9 Z" fill="${fill}"/></svg>`;
const occludedTriSvg = coldSvg;
// Stationary front: the proper WMO depiction is alternating cold/warm symbols on
// OPPOSITE sides of the line. This single 36x18 sprite carries one full period
// -- a cold triangle on the top half (apex up) and a warm semicircle on the
// bottom half (bulges down) -- so when symbol-placement:line repeats it at
// ~sprite-width spacing it tiles into triangle/semicircle/triangle/semicircle,
// each on its own side. (Arc sweep-flag 0 with left-to-right endpoints bulges
// +y = the bottom half, opposite the apex-up triangle.) The two colors are
// baked in, so unlike the single-type pips this is not parameterized by one fill.
const stationarySvg = (cold, warm) =>
  `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 36 18" width="36" height="18">` +
  `<polygon points="3,9 15,9 9,1.5" fill="${cold}"/>` +
  `<path d="M 21 9 A 6 6 0 0 0 33 9 Z" fill="${warm}"/></svg>`;

// Layer id sets. WPC keeps its original ids (stable); world mirrors them with a
// `fronts-world-` prefix. ALL_LAYER_IDS drives visibility/teardown over both.
// Built from a shared suffix list so the two sets stay in lockstep.
const LAYER_SUFFIXES = [
  'line',
  'stationary-line',
  'stationary-dash',
  'pips',
  'stationary-pips',
  'centers',
  'center-labels',
];
export const FRONT_LAYER_IDS = LAYER_SUFFIXES.map((s) => `fronts-${s}`);
export const FRONT_WORLD_LAYER_IDS = LAYER_SUFFIXES.map((s) => `fronts-world-${s}`);
const ALL_LAYER_IDS = [...FRONT_WORLD_LAYER_IDS, ...FRONT_LAYER_IDS];

// addImage ids for the colored pip sprites (one per front type, shared by both
// sources).
const IMG_COLD = 'front-cold';
const IMG_WARM = 'front-warm';
const IMG_OCCLUDED = 'front-occluded';
const IMG_STATIONARY = 'front-stationary';

// Rasterize sprites at this device-pixel multiple and register them with a
// matching `pixelRatio`, so the pips stay crisp at any zoom / icon-size instead
// of pixelating like a 1x bitmap upscaled. (A baked-color non-SDF sprite is
// just a bitmap, so DPI is the lever for sharpness.)
const SPRITE_PIXEL_RATIO = 4;

// Rasterize an SVG string into an ImageData of the given pixel size. Returns a
// Promise; resolves null in a non-DOM environment (e.g. node --test), where the
// overlay's icon layers simply render without sprites.
function rasterizeSvg(svg, w, h = w) {
  if (typeof document === 'undefined' || typeof Image === 'undefined') {
    return Promise.resolve(null);
  }
  return new Promise((resolve) => {
    const img = new Image();
    img.onload = () => {
      try {
        const canvas = document.createElement('canvas');
        canvas.width = w;
        canvas.height = h;
        const ctx = canvas.getContext('2d');
        ctx.drawImage(img, 0, 0, w, h);
        resolve(ctx.getImageData(0, 0, w, h));
      } catch {
        resolve(null);
      }
    };
    img.onerror = () => resolve(null);
    img.src = `data:image/svg+xml;charset=utf-8,${encodeURIComponent(svg)}`;
  });
}

// Catmull-Rom interpolation of one parametric coordinate at t in [0,1].
function catmull(p0, p1, p2, p3, t) {
  const t2 = t * t;
  const t3 = t2 * t;
  const f = (a, b, c, d) =>
    0.5 * (2 * b + (-a + c) * t + (2 * a - 5 * b + 4 * c - d) * t2 + (-a + 3 * b - 3 * c + d) * t3);
  return [f(p0[0], p1[0], p2[0], p3[0]), f(p0[1], p1[1], p2[1], p3[1])];
}

// Densify a [lon,lat][] polyline into a smooth Catmull-Rom curve. Each input
// segment is split into ceil(len/targetDeg) pieces (capped at maxSub), so the
// output is uniformly smooth regardless of the raw point spacing and the
// straight-chord segmentation disappears. Endpoints are preserved; <3 points
// (nothing to curve) pass through unchanged.
export function smoothLine(pts, targetDeg = 0.03, maxSub = 40) {
  if (!Array.isArray(pts) || pts.length < 3) return pts;
  const n = pts.length;
  const at = (i) => pts[Math.max(0, Math.min(n - 1, i))];
  const out = [];
  for (let i = 0; i < n - 1; i++) {
    const p0 = at(i - 1);
    const p1 = at(i);
    const p2 = at(i + 1);
    const p3 = at(i + 2);
    const segLen = Math.hypot(p2[0] - p1[0], p2[1] - p1[1]);
    const sub = Math.max(1, Math.min(maxSub, Math.ceil(segLen / targetDeg)));
    for (let s = 0; s < sub; s++) out.push(catmull(p0, p1, p2, p3, s / sub));
  }
  out.push(pts[n - 1]);
  return out;
}

// Absolute turn (radians) at vertex b between segments a->b and b->c.
function turnAt(a, b, c) {
  let t = Math.atan2(c[1] - b[1], c[0] - b[0]) - Math.atan2(b[1] - a[1], b[0] - a[0]);
  while (t > Math.PI) t -= 2 * Math.PI;
  while (t < -Math.PI) t += 2 * Math.PI;
  return Math.abs(t);
}

// Split a (dense, smoothed) line into the runs that are gentle enough to carry
// pips, dropping tight bends. At each vertex we sum the turning over a +/-
// windowDeg arc window (~a pip's footprint); where that exceeds maxTurn the
// curve is too tight for a pip to sit flat, so it's excluded. The result feeds
// the pip layers (via symbol-placement:line) so pips appear only on the gentle
// spans -- the tight curve still draws its line, just without clashing pips.
export function pipSpans(line, windowDeg = 0.3, maxTurn = 0.7) {
  if (!Array.isArray(line) || line.length < 2) return [];
  if (line.length < 3) return [line];
  const n = line.length;
  const cum = [0];
  for (let i = 1; i < n; i++) cum[i] = cum[i - 1] + Math.hypot(line[i][0] - line[i - 1][0], line[i][1] - line[i - 1][1]);
  const turn = new Array(n).fill(0);
  for (let i = 1; i < n - 1; i++) turn[i] = turnAt(line[i - 1], line[i], line[i + 1]);
  const excluded = new Array(n).fill(false);
  for (let i = 0; i < n; i++) {
    let s = 0;
    for (let j = i; j < n && cum[j] - cum[i] <= windowDeg / 2; j++) s += turn[j];
    for (let j = i - 1; j >= 0 && cum[i] - cum[j] <= windowDeg / 2; j--) s += turn[j];
    if (s > maxTurn) excluded[i] = true;
  }
  const spans = [];
  let run = [];
  for (let i = 0; i < n; i++) {
    if (!excluded[i]) run.push(line[i]);
    else { if (run.length >= 2) spans.push(run); run = []; }
  }
  if (run.length >= 2) spans.push(run);
  return spans;
}

// Return a copy of the FeatureCollection with every front LineString smoothed,
// plus a companion `pipline` MultiLineString per front carrying only the gentle
// spans (the pip layers follow these, so pips skip tight bends). Pressure-center
// Points are untouched. Tolerant of missing/empty input.
export function smoothFronts(fc, opts = {}) {
  const { targetDeg = 0.03, maxSub = 40, pipWindowDeg = 0.3, pipMaxTurn = 0.7 } = opts;
  if (!fc || !Array.isArray(fc.features)) return fc;
  const out = [];
  for (const f of fc.features) {
    if (f?.properties?.feature !== 'front' || f?.geometry?.type !== 'LineString') {
      out.push(f);
      continue;
    }
    const smoothed = smoothLine(f.geometry.coordinates, targetDeg, maxSub);
    out.push({ ...f, geometry: { ...f.geometry, coordinates: smoothed } });
    const spans = pipSpans(smoothed, pipWindowDeg, pipMaxTurn);
    if (spans.length) {
      out.push({
        type: 'Feature',
        properties: { ...f.properties, feature: 'pipline' },
        geometry: { type: 'MultiLineString', coordinates: spans },
      });
    }
  }
  return { ...fc, features: out };
}

const EMPTY_FC = { type: 'FeatureCollection', features: [] };

// line-color match for the cold/warm/occluded/trough base line. stationary is
// not here -- it draws via its own two-tone (blue base + red dash) layers.
function frontColorMatch() {
  return [
    'match',
    ['get', 'front_type'],
    'cold', FRONT_COLORS.cold,
    'warm', FRONT_COLORS.warm,
    'occluded', FRONT_COLORS.occluded,
    'trough', FRONT_COLORS.trough,
    '#888888',
  ];
}

// Pip sprite per front type. cold/warm/occluded carry pips; trough and
// stationary resolve to '' (no icon) -- stationary draws its own combined
// sprite, trough carries no pips.
function pipIconMatch() {
  return [
    'match',
    ['get', 'front_type'],
    'cold', IMG_COLD,
    'warm', IMG_WARM,
    'occluded', IMG_OCCLUDED,
    '',
  ];
}

// Build the seven layer definitions for a source, with ids prefixed by idPrefix
// ('fronts' or 'fronts-world'). Identical paint/layout for both sources.
//
// WMO symbology notes:
//  - cold/warm/occluded carry a single repeated sprite along the line; occluded
//    uses the cold triangle only (its alternating triangle/semicircle is still
//    deferred). trough is dashed and carries no pips.
//  - stationary needs cold/warm symbology on OPPOSITE sides, which a single
//    MapLibre line layer can't do, so it draws as a solid cold-blue base with a
//    warm-red dashed line on top (alternating red/blue segments) plus a combined
//    2-symbol sprite for the alternating opposite-side pips.
// The pip layers follow the smoothed `pipline` geometry (gentle spans only) so
// pips skip tight bends; the line layers follow the full `front` geometry.
function layerSpecs(sourceId, idPrefix, vis) {
  const lineWidth = ['interpolate', ['linear'], ['zoom'], 3, 2.5, 8, 5];
  const stationaryFilter = [
    'all',
    ['==', ['get', 'feature'], 'front'],
    ['==', ['get', 'front_type'], 'stationary'],
  ];
  return [
    // 1) Base front line for cold/warm/occluded/trough. trough is dashed; every
    // other type is solid. stationary is excluded (its own two-tone layers).
    {
      id: `${idPrefix}-line`,
      type: 'line',
      source: sourceId,
      filter: [
        'all',
        ['==', ['get', 'feature'], 'front'],
        ['!=', ['get', 'front_type'], 'stationary'],
      ],
      layout: { visibility: vis, 'line-cap': 'round', 'line-join': 'round' },
      paint: {
        'line-color': frontColorMatch(),
        // Dash units are line-width-relative, so troughs' dashes scale with
        // this. Tunable live via window.gwFronts.tune({ lineWidth }).
        'line-width': lineWidth,
        'line-dasharray': [
          'case',
          ['==', ['get', 'front_type'], 'trough'],
          ['literal', [2, 2]],
          ['literal', [1]],
        ],
      },
    },
    // 1b) Stationary front: solid cold-blue base...
    {
      id: `${idPrefix}-stationary-line`,
      type: 'line',
      source: sourceId,
      filter: stationaryFilter,
      layout: { visibility: vis, 'line-cap': 'butt', 'line-join': 'round' },
      paint: { 'line-color': FRONT_COLORS.cold, 'line-width': lineWidth },
    },
    // ...with a warm-red dashed line on top, so the gaps reveal blue and the
    // line reads as alternating red/blue (the WMO two-tone stationary boundary).
    {
      id: `${idPrefix}-stationary-dash`,
      type: 'line',
      source: sourceId,
      filter: stationaryFilter,
      layout: { visibility: vis, 'line-cap': 'butt', 'line-join': 'round' },
      paint: {
        'line-color': FRONT_COLORS.warm,
        'line-width': lineWidth,
        // Equal dash/gap: red covers half the line, blue base shows through
        // the gaps -> alternating red/blue.
        'line-dasharray': ['literal', [3, 3]],
      },
    },
    // 2) Frontal pips along the gentle spans. cold/warm/occluded only.
    {
      id: `${idPrefix}-pips`,
      type: 'symbol',
      source: sourceId,
      filter: [
        'all',
        ['==', ['get', 'feature'], 'pipline'],
        ['!=', ['get', 'front_type'], 'trough'],
        ['!=', ['get', 'front_type'], 'stationary'],
      ],
      layout: {
        visibility: vis,
        'symbol-placement': 'line',
        'symbol-spacing': ['interpolate', ['linear'], ['zoom'], 3, 30, 8, 60],
        'icon-image': pipIconMatch(),
        // Tunable live via window.gwFronts.tune().
        'icon-size': ['interpolate', ['linear'], ['zoom'], 3, 0.7, 8, 1.0],
        'icon-rotation-alignment': 'map',
        // allow-overlap OFF (and no ignore-placement) so MapLibre drops pips it
        // can't lay flat on a tight curve -- those are the ones that dangle off
        // the bends -- instead of force-rendering them.
        'icon-allow-overlap': false,
      },
    },
    // 2b) Stationary pips. The combined cold-triangle/warm-semicircle sprite is
    // repeated along the line at roughly its own width, so the two symbols tile
    // into the alternating opposite-side pattern.
    {
      id: `${idPrefix}-stationary-pips`,
      type: 'symbol',
      source: sourceId,
      filter: [
        'all',
        ['==', ['get', 'feature'], 'pipline'],
        ['==', ['get', 'front_type'], 'stationary'],
      ],
      layout: {
        visibility: vis,
        'symbol-placement': 'line',
        // Repeat at ~the rendered sprite width (36px * icon-size) so the sprites
        // abut and the triangle/semicircle stay evenly spaced (even alternation)
        // rather than clustering with gaps between units.
        'symbol-spacing': ['interpolate', ['linear'], ['zoom'], 3, 26, 8, 36],
        'icon-image': IMG_STATIONARY,
        'icon-size': ['interpolate', ['linear'], ['zoom'], 3, 0.7, 8, 1.0],
        'icon-rotation-alignment': 'map',
        'icon-keep-upright': false,
        // See cold/warm pips: drop rather than dangle on tight curves.
        'icon-allow-overlap': false,
      },
    },
    // 3) Pressure-center glyphs (H / L). Blue H, red L, white halo for contrast
    // on either basemap.
    {
      id: `${idPrefix}-centers`,
      type: 'symbol',
      source: sourceId,
      filter: ['==', ['get', 'feature'], 'center'],
      layout: {
        visibility: vis,
        'text-field': ['get', 'kind'],
        'text-font': ['Open Sans Bold', 'Arial Unicode MS Bold'],
        'text-size': ['interpolate', ['linear'], ['zoom'], 3, 16, 8, 28],
        'text-allow-overlap': true,
        'text-ignore-placement': true,
      },
      paint: {
        'text-color': [
          'match',
          ['get', 'kind'],
          'H', FRONT_COLORS.cold,
          'L', FRONT_COLORS.warm,
          '#333333',
        ],
        'text-halo-color': '#ffffff',
        'text-halo-width': 2,
      },
    },
    // 4) Center pressure label (mb) below the H/L glyph.
    {
      id: `${idPrefix}-center-labels`,
      type: 'symbol',
      source: sourceId,
      filter: ['==', ['get', 'feature'], 'center'],
      layout: {
        visibility: vis,
        'text-field': ['to-string', ['get', 'pressure_mb']],
        'text-font': ['Open Sans Semibold', 'Arial Unicode MS Regular'],
        'text-size': ['interpolate', ['linear'], ['zoom'], 3, 10, 8, 14],
        'text-offset': [0, 1.2],
        'text-anchor': 'top',
        'text-allow-overlap': true,
        'text-ignore-placement': true,
      },
      paint: {
        'text-color': '#333333',
        'text-halo-color': '#ffffff',
        'text-halo-width': 1.5,
      },
    },
  ];
}

export function mountFrontsLayer(map, { visible }) {
  const wpc = frontsProvider();
  const world = frontsWorldProvider();
  let curVisible = visible;
  // Smoothed FeatureCollection currently fed to each source. LiveMapV2 fetches
  // the raw documents (it holds the bearer token) and pushes them via setData()
  // / setWorldData(); we Catmull-Rom the lines before handing them to MapLibre
  // so the overlay renders smooth curves and the pip icons sit flush instead of
  // dangling off the straight-chord corners. Start empty until the first push.
  let curData = EMPTY_FC; // WPC, for info()
  let lastRawWpc = EMPTY_FC;
  let lastRawWorld = EMPTY_FC;
  let smoothTargetDeg = 0.03; // Catmull-Rom resample target; smaller = curvier
  let pipWindowDeg = 0.3; // arc window for the tight-bend pip test
  let pipMaxTurn = 0.7; // radians of turn over that window before pips are dropped

  const firstSymbolId = () => map.getStyle().layers.find((l) => l.type === 'symbol')?.id;

  // Load the pip sprites once, each baked with its front-type color. Guarded by
  // map.hasImage so a style swap (which drops user images) re-registers them.
  async function loadImages() {
    // [id, svg, width, height] -- stationary is a wide 2-symbol sprite (36x18);
    // the single-type pips are square (18x18).
    const want = [
      [IMG_COLD, coldSvg(FRONT_COLORS.cold), 18, 18],
      [IMG_WARM, warmSvg(FRONT_COLORS.warm), 18, 18],
      [IMG_OCCLUDED, occludedTriSvg(FRONT_COLORS.occluded), 18, 18],
      [IMG_STATIONARY, stationarySvg(FRONT_COLORS.cold, FRONT_COLORS.warm), 36, 18],
    ];
    for (const [id, svg, w, h] of want) {
      if (map.hasImage && map.hasImage(id)) continue;
      // Rasterize at SPRITE_PIXEL_RATIO x the logical size for crispness.
      const data = await rasterizeSvg(svg, w * SPRITE_PIXEL_RATIO, h * SPRITE_PIXEL_RATIO);
      if (!data) continue;
      if (map.hasImage && map.hasImage(id)) continue;
      // Non-SDF: color is baked into the sprite, so no runtime icon-color tint.
      // pixelRatio tells MapLibre this bitmap is hi-DPI, so icon-size still maps
      // to the logical w/h and the extra pixels just sharpen it.
      map.addImage(id, data, { sdf: false, pixelRatio: SPRITE_PIXEL_RATIO });
    }
  }

  function addLayers(specs, beforeId) {
    for (const spec of specs) {
      if (!map.getLayer(spec.id)) map.addLayer(spec, beforeId);
    }
  }

  function ensure() {
    // Sources are pushed (and smoothed) via setData()/setWorldData(); seed each
    // with the current smoothed document so a style swap re-adds it without a
    // flash.
    if (!map.getSource(wpc.sourceId)) {
      map.addSource(wpc.sourceId, { type: 'geojson', data: curData });
    }
    if (!map.getSource(world.sourceId)) {
      map.addSource(world.sourceId, { type: 'geojson', data: smoothCurrent(lastRawWorld) });
    }
    const vis = curVisible ? 'visible' : 'none';
    const firstSym = firstSymbolId();

    // WPC first, above the basemap symbol layers.
    addLayers(layerSpecs(wpc.sourceId, 'fronts', vis), firstSym);
    // World beneath WPC: insert before the WPC base line if present, else above
    // the basemap symbols. This keeps the analyst product on top over NA.
    const worldBefore = map.getLayer('fronts-line') ? 'fronts-line' : firstSym;
    addLayers(layerSpecs(world.sourceId, 'fronts-world', vis), worldBefore);
  }

  // Register sprites (async, best-effort) then add layers.
  loadImages();
  ensure();

  // Re-add source/layers + re-register sprites behind existence guards so the
  // overlay survives a basemap setStyle().
  function refresh() {
    loadImages();
    ensure();
  }

  function smoothCurrent(rawFc) {
    return smoothFronts(rawFc, { targetDeg: smoothTargetDeg, pipWindowDeg, pipMaxTurn }) ?? EMPTY_FC;
  }

  // Accept a freshly fetched (raw) GeoJSON document, smooth its front lines, and
  // hand it to the WPC source. No source/layer churn. Called by LiveMapV2 on
  // initial load and whenever the manifest poll sees a new WPC analysis.
  function setData(rawFc) {
    lastRawWpc = rawFc ?? EMPTY_FC;
    curData = smoothCurrent(lastRawWpc);
    map.getSource(FRONTS_SOURCE_ID)?.setData(curData);
  }

  // Same for the model-derived global (world) source.
  function setWorldData(rawFc) {
    lastRawWorld = rawFc ?? EMPTY_FC;
    map.getSource(FRONTS_WORLD_SOURCE_ID)?.setData(smoothCurrent(lastRawWorld));
  }

  // Re-run smoothing on both sources' last raw documents (used by tune()).
  function applySmoothed() {
    setData(lastRawWpc);
    setWorldData(lastRawWorld);
  }

  function setVisible(v) {
    curVisible = v;
    const value = v ? 'visible' : 'none';
    for (const id of ALL_LAYER_IDS) {
      if (map.getLayer(id)) map.setLayoutProperty(id, 'visibility', value);
    }
  }

  function destroy() {
    try {
      for (const id of ALL_LAYER_IDS) {
        if (map.getLayer(id)) map.removeLayer(id);
      }
      if (map.getSource(FRONTS_SOURCE_ID)) map.removeSource(FRONTS_SOURCE_ID);
      if (map.getSource(FRONTS_WORLD_SOURCE_ID)) map.removeSource(FRONTS_WORLD_SOURCE_ID);
    } catch { /* map already removed */ }
  }

  // Live visual tuning from the browser console (exposed via window.gwFronts by
  // LiveMapV2). Adjust any knob and watch the map, then report the values that
  // look right so they can be baked into the defaults above. Examples:
  //   window.gwFronts.tune({ pipSize: 0.7, pipSpacing: 40 })
  //   window.gwFronts.tune({ statSize: 0.8, statSpacing: 30, lineWidth: 3 })
  // A number applies at all zooms; pass a MapLibre expression for a zoom ramp.
  // Knobs fan out across both sources' layers.
  function tune(o = {}) {
    const idsFor = (suffix) => [`fronts-${suffix}`, `fronts-world-${suffix}`];
    const sl = (id, p, v) => { if (v != null && map.getLayer(id)) map.setLayoutProperty(id, p, v); };
    const sp = (id, p, v) => { if (v != null && map.getLayer(id)) map.setPaintProperty(id, p, v); };
    const slAll = (suffix, p, v) => { for (const id of idsFor(suffix)) sl(id, p, v); };
    const spAll = (suffix, p, v) => { for (const id of idsFor(suffix)) sp(id, p, v); };
    const pipSuffixes = ['pips', 'stationary-pips'];
    if (o.lineWidth != null) {
      for (const suffix of ['line', 'stationary-line', 'stationary-dash']) {
        spAll(suffix, 'line-width', o.lineWidth);
      }
    }
    slAll('pips', 'icon-size', o.pipSize);
    slAll('pips', 'symbol-spacing', o.pipSpacing);
    slAll('stationary-pips', 'icon-size', o.statSize);
    slAll('stationary-pips', 'symbol-spacing', o.statSpacing);
    // Placement flags drive the dangling fix: with overlap/ignore-placement
    // OFF, MapLibre drops pips it can't lay cleanly on a curve (the ones that
    // dangle at bends) instead of force-rendering them.
    if (o.allowOverlap != null) for (const s of pipSuffixes) slAll(s, 'icon-allow-overlap', o.allowOverlap);
    if (o.ignorePlacement != null) for (const s of pipSuffixes) slAll(s, 'icon-ignore-placement', o.ignorePlacement);
    // Re-process the geometry. smoothTarget = chord size (smaller = curvier).
    // pipWindow/pipMaxTurn control the tight-bend pip dropout: a pip is dropped
    // where the line turns more than pipMaxTurn radians across a +/-pipWindow
    // arc. Lower pipMaxTurn (or larger pipWindow) drops pips on gentler bends.
    if (o.smoothTarget != null) smoothTargetDeg = o.smoothTarget;
    if (o.pipWindow != null) pipWindowDeg = o.pipWindow;
    if (o.pipMaxTurn != null) pipMaxTurn = o.pipMaxTurn;
    if (o.smoothTarget != null || o.pipWindow != null || o.pipMaxTurn != null) applySmoothed();
    return { ...o, smoothTargetDeg, pipWindowDeg, pipMaxTurn };
  }

  // Snapshot of what's currently rendered, for console diagnostics.
  function info() {
    const fronts = (curData?.features ?? []).filter((f) => f?.properties?.feature === 'front');
    return {
      zoom: map.getZoom?.(),
      smoothTargetDeg,
      pipWindowDeg,
      pipMaxTurn,
      frontFeatures: fronts.length,
      maxLinePoints: fronts.reduce((m, f) => Math.max(m, f.geometry?.coordinates?.length ?? 0), 0),
      stationarySpriteLoaded: map.hasImage?.(IMG_STATIONARY) ?? null,
    };
  }

  return { setVisible, refresh, setData, setWorldData, destroy, tune, info };
}
