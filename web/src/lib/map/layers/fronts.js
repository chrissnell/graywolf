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

export const FRONT_LAYER_IDS = [
  'fronts-line',
  'fronts-pips',
  'fronts-centers',
  'fronts-center-labels',
];

// addImage ids for the colored pip sprites (one per front type).
const IMG_COLD = 'front-cold';
const IMG_WARM = 'front-warm';
const IMG_OCCLUDED = 'front-occluded';

// Rasterize an SVG string into an ImageData of the given pixel size. Returns a
// Promise; resolves null in a non-DOM environment (e.g. node --test), where the
// overlay's icon layers simply render without sprites.
function rasterizeSvg(svg, size) {
  if (typeof document === 'undefined' || typeof Image === 'undefined') {
    return Promise.resolve(null);
  }
  return new Promise((resolve) => {
    const img = new Image();
    img.onload = () => {
      try {
        const canvas = document.createElement('canvas');
        canvas.width = size;
        canvas.height = size;
        const ctx = canvas.getContext('2d');
        ctx.drawImage(img, 0, 0, size, size);
        resolve(ctx.getImageData(0, 0, size, size));
      } catch {
        resolve(null);
      }
    };
    img.onerror = () => resolve(null);
    img.src = `data:image/svg+xml;charset=utf-8,${encodeURIComponent(svg)}`;
  });
}

export function mountFrontsLayer(map, { visible }) {
  const provider = frontsProvider();
  let curVisible = visible;

  const firstSymbolId = () => map.getStyle().layers.find((l) => l.type === 'symbol')?.id;

  // Load the pip sprites once, each baked with its front-type color. Guarded by
  // map.hasImage so a style swap (which drops user images) re-registers them on
  // the next ensure().
  async function loadImages() {
    const want = [
      [IMG_COLD, coldSvg(FRONT_COLORS.cold)],
      [IMG_WARM, warmSvg(FRONT_COLORS.warm)],
      [IMG_OCCLUDED, occludedTriSvg(FRONT_COLORS.occluded)],
    ];
    for (const [id, svg] of want) {
      if (map.hasImage && map.hasImage(id)) continue;
      const data = await rasterizeSvg(svg, 18);
      if (!data) continue;
      // hasImage re-checked after the await -- a concurrent ensure() or another
      // mount may have registered it while we were rasterizing.
      if (map.hasImage && map.hasImage(id)) continue;
      // Non-SDF: color is baked into the sprite, so no runtime icon-color tint.
      map.addImage(id, data, { sdf: false });
    }
  }

  // line-color match on the feature's front_type.
  const frontColorMatch = () => [
    'match',
    ['get', 'front_type'],
    'cold', FRONT_COLORS.cold,
    'warm', FRONT_COLORS.warm,
    'stationary', FRONT_COLORS.stationary,
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
      map.addSource(provider.sourceId, provider.source);
    }
    const beforeId = firstSymbolId();
    const vis = curVisible ? 'visible' : 'none';

    // 1) Base front line. trough is dashed; every other type is solid. The dash
    // array is gated on front_type so only troughs get it.
    if (!map.getLayer('fronts-line')) {
      map.addLayer(
        {
          id: 'fronts-line',
          type: 'line',
          source: provider.sourceId,
          filter: ['==', ['get', 'feature'], 'front'],
          layout: {
            visibility: vis,
            'line-cap': 'round',
            'line-join': 'round',
          },
          paint: {
            'line-color': frontColorMatch(),
            'line-width': ['interpolate', ['linear'], ['zoom'], 3, 1.2, 8, 2.6],
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

    // 2) Frontal pips along the line.
    //
    // v1 LIMITATION (documented, not an oversight): MapLibre
    // symbol-placement:line draws a single sprite repeated on ONE side of the
    // line, so it cannot render a stationary front's alternating
    // opposite-side warm/cold pips, nor an occluded front's alternating
    // triangle/semicircle. So this layer deliberately EXCLUDES stationary (its
    // line still renders, just without pips) and occluded uses the cold
    // triangle only. Proper alternating-side symbology needs either two offset
    // symbol layers with side-tagged geometry from the generator, or a custom
    // WebGL layer -- deferred past v1.
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
            'symbol-spacing': ['interpolate', ['linear'], ['zoom'], 3, 28, 8, 60],
            'icon-image': pipIconMatch(),
            'icon-size': ['interpolate', ['linear'], ['zoom'], 3, 0.7, 8, 1.0],
            'icon-rotation-alignment': 'map',
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

  // Re-fetch the GeoJSON document (new analysis published). No source/layer
  // churn -- just hand the source new data.
  function reload() {
    map.getSource(FRONTS_SOURCE_ID)?.setData(provider.dataUrl);
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

  return { setVisible, refresh, reload, destroy };
}
