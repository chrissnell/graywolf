// Surface-fronts overlay layer for the Live Map (WMO frontal symbology).
//
// Backend-agnostic in the same spirit as radar.js: it asks fronts-source.js for
// a provider descriptor and performs the MapLibre source/layer calls. Unlike
// radar there is no per-frame loop -- a single GeoJSON document holds the
// current analysis (fronts + pressure centers), and the overlay renders
// whatever features it carries. A slow manifest poll (driven by LiveMapV2)
// calls reload() when a new analysis is published.
//
// Mirrors the other layer modules (radar.js, stations.js, trails.js): mount
// returns control methods; LiveMapV2 persists settings and drives them via
// effects, and calls refresh() on every data tick. refresh() re-adds the
// source/layers behind existence guards so the overlay survives a basemap
// setStyle() (which rebuilds the style and can drop user-added layers).
//
// Frontal pips are sprite icons placed along the line (symbol-placement:line).
// One colored sprite is baked per front type at registration time (the fill is
// parameterized with the front-type color, then rasterized as a normal non-SDF
// image). Earlier versions registered a single black silhouette as an SDF image
// tinted at runtime via icon-color, but MapLibre's sdf flag reads the alpha
// channel as a signed distance field -- a hard-rasterized binary mask is not a
// distance field, so tinting fringed the edges at interpolated icon-size.

import { frontsProvider, FRONTS_SOURCE_ID, FRONT_COLORS } from '../sources/fronts-source.js';

// Pip glyph markup. Kept inline (not a Vite `?raw` import) so this module loads
// unchanged under plain `node --test`, which has no Vite to resolve `?raw`. The
// canonical, hand-editable copies live alongside as SVG files -- keep these in
// sync with them:
//   ../style/front-sprites/cold.svg       (cold triangle, base on baseline,
//                                           points up)
//   ../style/front-sprites/warm.svg       (warm semicircle, flat edge on
//                                           baseline)
//   ../style/front-sprites/occluded-tri.svg  (same triangle, used for occluded)
// The canonical SVGs are single-color sources of truth for the glyph shapes;
// the fill is parameterized below so each front type bakes its own color.
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

export const FRONT_LAYER_IDS = [
  'fronts-line',
  'fronts-stationary-line',
  'fronts-stationary-dash',
  'fronts-pips',
  'fronts-stationary-pips',
  'fronts-centers',
  'fronts-center-labels',
];

// addImage ids for the colored pip sprites (one per front type).
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
export function smoothLine(pts, targetDeg = 0.15, maxSub = 10) {
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

// Return a copy of the FeatureCollection with every front LineString smoothed.
// Pressure-center Points are untouched. Tolerant of missing/empty input.
export function smoothFronts(fc) {
  if (!fc || !Array.isArray(fc.features)) return fc;
  const features = fc.features.map((f) => {
    if (f?.properties?.feature !== 'front' || f?.geometry?.type !== 'LineString') return f;
    return { ...f, geometry: { ...f.geometry, coordinates: smoothLine(f.geometry.coordinates) } };
  });
  return { ...fc, features };
}

const EMPTY_FC = { type: 'FeatureCollection', features: [] };

export function mountFrontsLayer(map, { visible }) {
  const provider = frontsProvider();
  let curVisible = visible;
  // Smoothed FeatureCollection currently fed to the source. LiveMapV2 fetches
  // the raw document (it holds the bearer token) and pushes it via setData();
  // we Catmull-Rom the lines before handing them to MapLibre so the overlay
  // renders smooth curves and the pip icons sit flush instead of dangling off
  // the straight-chord corners. Starts empty until the first push.
  let curData = EMPTY_FC;

  const firstSymbolId = () => map.getStyle().layers.find((l) => l.type === 'symbol')?.id;

  // Load the pip sprites once, each baked with its front-type color. Guarded by
  // map.hasImage so a style swap (which drops user images) re-registers them on
  // the next ensure().
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
      // hasImage re-checked after the await -- a concurrent ensure() or another
      // mount may have registered it while we were rasterizing.
      if (map.hasImage && map.hasImage(id)) continue;
      // Non-SDF: color is baked into the sprite, so no runtime icon-color tint.
      // pixelRatio tells MapLibre this bitmap is hi-DPI, so icon-size still maps
      // to the logical w/h and the extra pixels just sharpen it.
      map.addImage(id, data, { sdf: false, pixelRatio: SPRITE_PIXEL_RATIO });
    }
  }

  // line-color match for the cold/warm/occluded/trough base line. stationary is
  // not here -- it draws via its own two-tone (blue base + red dash) layers.
  const frontColorMatch = () => [
    'match',
    ['get', 'front_type'],
    'cold', FRONT_COLORS.cold,
    'warm', FRONT_COLORS.warm,
    'occluded', FRONT_COLORS.occluded,
    'trough', FRONT_COLORS.trough,
    '#888888',
  ];

  // Pip sprite per front type. cold/warm/occluded carry pips; trough and
  // stationary resolve to '' (no icon) -- see the v1 limitation note below.
  const pipIconMatch = () => [
    'match',
    ['get', 'front_type'],
    'cold', IMG_COLD,
    'warm', IMG_WARM,
    'occluded', IMG_OCCLUDED,
    '',
  ];

  function ensure() {
    if (!map.getSource(provider.sourceId)) {
      // Source data is pushed (and smoothed) via setData(); seed it with the
      // current smoothed document so a style swap re-adds it without a flash.
      map.addSource(provider.sourceId, { type: 'geojson', data: curData });
    }
    const beforeId = firstSymbolId();
    const vis = curVisible ? 'visible' : 'none';

    // 1) Base front line for cold/warm/occluded/trough. trough is dashed; every
    // other type is solid. stationary is handled by its own two-tone line +
    // alternating-pip layers below (it needs cold/warm symbology on opposite
    // sides), so it is excluded here.
    if (!map.getLayer('fronts-line')) {
      map.addLayer(
        {
          id: 'fronts-line',
          type: 'line',
          source: provider.sourceId,
          filter: [
            'all',
            ['==', ['get', 'feature'], 'front'],
            ['!=', ['get', 'front_type'], 'stationary'],
          ],
          layout: {
            visibility: vis,
            'line-cap': 'round',
            'line-join': 'round',
          },
          paint: {
            'line-color': frontColorMatch(),
            // A touch thicker than v1 so the boundaries read clearly. Dash units
            // are line-width-relative, so troughs' dashes scale up with this.
            'line-width': ['interpolate', ['linear'], ['zoom'], 3, 1.8, 8, 3.8],
            'line-dasharray': [
              'case',
              ['==', ['get', 'front_type'], 'trough'],
              ['literal', [2, 2]],
              ['literal', [1]],
            ],
          },
        },
        beforeId,
      );
    }

    // 1b) Stationary front line: a solid cold-blue base with a warm-red dashed
    // line painted on top, so the gaps reveal blue and the line reads as
    // alternating red/blue segments -- the WMO two-tone stationary boundary --
    // without needing per-segment color (which MapLibre line layers can't do).
    const stationaryFilter = [
      'all',
      ['==', ['get', 'feature'], 'front'],
      ['==', ['get', 'front_type'], 'stationary'],
    ];
    const stationaryWidth = ['interpolate', ['linear'], ['zoom'], 3, 1.8, 8, 3.8];
    if (!map.getLayer('fronts-stationary-line')) {
      map.addLayer(
        {
          id: 'fronts-stationary-line',
          type: 'line',
          source: provider.sourceId,
          filter: stationaryFilter,
          layout: { visibility: vis, 'line-cap': 'butt', 'line-join': 'round' },
          paint: { 'line-color': FRONT_COLORS.cold, 'line-width': stationaryWidth },
        },
        beforeId,
      );
    }
    if (!map.getLayer('fronts-stationary-dash')) {
      map.addLayer(
        {
          id: 'fronts-stationary-dash',
          type: 'line',
          source: provider.sourceId,
          filter: stationaryFilter,
          layout: { visibility: vis, 'line-cap': 'butt', 'line-join': 'round' },
          paint: {
            'line-color': FRONT_COLORS.warm,
            'line-width': stationaryWidth,
            // Equal dash/gap: red covers half the line, blue base shows through
            // the gaps -> alternating red/blue.
            'line-dasharray': ['literal', [3, 3]],
          },
        },
        beforeId,
      );
    }

    // 2) Frontal pips along the line.
    //
    // cold/warm/occluded carry a single repeated sprite. occluded uses the cold
    // triangle only (its alternating triangle/semicircle is still deferred).
    // stationary is handled separately (layer 2b) with a combined 2-symbol
    // sprite that produces the proper alternating opposite-side pips.
    if (!map.getLayer('fronts-pips')) {
      map.addLayer(
        {
          id: 'fronts-pips',
          type: 'symbol',
          source: provider.sourceId,
          filter: [
            'all',
            ['==', ['get', 'feature'], 'front'],
            ['!=', ['get', 'front_type'], 'trough'],
            ['!=', ['get', 'front_type'], 'stationary'],
          ],
          layout: {
            visibility: vis,
            'symbol-placement': 'line',
            // Slightly wider spacing to keep the larger pips from crowding.
            'symbol-spacing': ['interpolate', ['linear'], ['zoom'], 3, 34, 8, 70],
            'icon-image': pipIconMatch(),
            // A little larger than v1 so the triangles/semicircles read clearly.
            'icon-size': ['interpolate', ['linear'], ['zoom'], 3, 0.95, 8, 1.35],
            'icon-rotation-alignment': 'map',
            'icon-allow-overlap': true,
            'icon-ignore-placement': true,
          },
        },
        beforeId,
      );
    }

    // 2b) Stationary pips. The combined cold-triangle/warm-semicircle sprite is
    // repeated along the line at roughly its own width, so the two symbols tile
    // into the alternating opposite-side pattern. symbol-spacing is matched to
    // the sprite footprint (wider than the single-type pips since each unit is
    // two symbols) to keep the pattern continuous without overlap.
    if (!map.getLayer('fronts-stationary-pips')) {
      map.addLayer(
        {
          id: 'fronts-stationary-pips',
          type: 'symbol',
          source: provider.sourceId,
          filter: stationaryFilter,
          layout: {
            visibility: vis,
            'symbol-placement': 'line',
            // Repeat at ~the rendered sprite width (36px * icon-size) so the
            // sprites abut and the triangle/semicircle stay evenly spaced
            // (even alternation) rather than clustering with gaps between units.
            'symbol-spacing': ['interpolate', ['linear'], ['zoom'], 3, 34, 8, 49],
            'icon-image': IMG_STATIONARY,
            'icon-size': ['interpolate', ['linear'], ['zoom'], 3, 0.95, 8, 1.35],
            'icon-rotation-alignment': 'map',
            'icon-keep-upright': false,
            'icon-allow-overlap': true,
            'icon-ignore-placement': true,
          },
        },
        beforeId,
      );
    }

    // 3) Pressure-center glyphs (H / L). Blue H, red L, white halo for contrast
    // on either basemap.
    if (!map.getLayer('fronts-centers')) {
      map.addLayer(
        {
          id: 'fronts-centers',
          type: 'symbol',
          source: provider.sourceId,
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
        beforeId,
      );
    }

    // 4) Center pressure label (mb) below the H/L glyph.
    if (!map.getLayer('fronts-center-labels')) {
      map.addLayer(
        {
          id: 'fronts-center-labels',
          type: 'symbol',
          source: provider.sourceId,
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
        beforeId,
      );
    }
  }

  // Register sprites (async, best-effort) then add layers. ensure() is safe
  // before the images resolve: an icon-image that isn't loaded yet simply
  // renders nothing until addImage lands, and MapLibre re-evaluates.
  loadImages();
  ensure();

  // Re-add source/layers + re-register sprites behind existence guards so the
  // overlay survives a basemap setStyle().
  function refresh() {
    loadImages();
    ensure();
  }

  // Accept a freshly fetched (raw) GeoJSON document, smooth its front lines,
  // and hand it to the source. No source/layer churn. Called by LiveMapV2 on
  // initial load and whenever the manifest poll sees a new analysis.
  function setData(rawFc) {
    curData = smoothFronts(rawFc) ?? EMPTY_FC;
    map.getSource(FRONTS_SOURCE_ID)?.setData(curData);
  }

  function setVisible(v) {
    curVisible = v;
    const value = v ? 'visible' : 'none';
    for (const id of FRONT_LAYER_IDS) {
      if (map.getLayer(id)) map.setLayoutProperty(id, 'visibility', value);
    }
  }

  function destroy() {
    try {
      for (const id of FRONT_LAYER_IDS) {
        if (map.getLayer(id)) map.removeLayer(id);
      }
      if (map.getSource(FRONTS_SOURCE_ID)) map.removeSource(FRONTS_SOURCE_ID);
    } catch { /* map already removed */ }
  }

  return { setVisible, refresh, setData, destroy };
}
