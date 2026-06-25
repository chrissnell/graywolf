// Surface-fronts overlay layer for the Live Map (WMO frontal symbology).
//
// Backend-agnostic in the same spirit as radar.js: it asks fronts-source.js for
// provider descriptors and performs the MapLibre source/layer calls. Unlike
// radar there is no per-frame loop -- a single GeoJSON document per source holds
// the current analysis (fronts + pressure centers), and the overlay renders
// whatever features it carries. A slow manifest poll (driven by LiveMapV2)
// calls reload() when a new analysis is published.
//
// TWO sources, one toggle:
//   fronts       -- WPC coded surface bulletin (analysis, North America)
//   fronts-world -- model-derived global fronts (GFS Thermal Front Parameter)
// Both render with identical styling (same paint/layout, same pip sprites). The
// world layers are inserted BENEATH the WPC layers so the analyst product wins
// over North America and the model shows through everywhere else. setVisible /
// refresh / reload / destroy fan out to both.
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

// Layer id sets. WPC keeps its original ids (stable); world mirrors them with a
// `fronts-world-` prefix. ALL_LAYER_IDS drives visibility/teardown over both.
export const FRONT_LAYER_IDS = [
  'fronts-line',
  'fronts-pips',
  'fronts-centers',
  'fronts-center-labels',
];
export const FRONT_WORLD_LAYER_IDS = [
  'fronts-world-line',
  'fronts-world-pips',
  'fronts-world-centers',
  'fronts-world-center-labels',
];
const ALL_LAYER_IDS = [...FRONT_WORLD_LAYER_IDS, ...FRONT_LAYER_IDS];

// addImage ids for the colored pip sprites (one per front type, shared by both
// sources).
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

// line-color match on the feature's front_type (source-independent).
function frontColorMatch() {
  return [
    'match',
    ['get', 'front_type'],
    'cold', FRONT_COLORS.cold,
    'warm', FRONT_COLORS.warm,
    'stationary', FRONT_COLORS.stationary,
    'occluded', FRONT_COLORS.occluded,
    'trough', FRONT_COLORS.trough,
    '#888888',
  ];
}

// Pip sprite per front type. cold/warm/occluded carry pips; trough and
// stationary resolve to '' (no icon) -- see the v1 limitation note below.
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

// Build the four layer definitions for a source, with ids prefixed by idPrefix
// ('fronts' or 'fronts-world'). Identical paint/layout for both sources.
//
// v1 LIMITATION (documented, not an oversight): MapLibre symbol-placement:line
// draws a single sprite repeated on ONE side of the line, so it cannot render a
// stationary front's alternating opposite-side pips, nor an occluded front's
// alternating triangle/semicircle. So the pip layer EXCLUDES stationary (its
// line still renders, just without pips) and occluded uses the cold triangle
// only. Proper alternating-side symbology is deferred past v1.
function layerSpecs(sourceId, idPrefix, vis) {
  return [
    {
      id: `${idPrefix}-line`,
      type: 'line',
      source: sourceId,
      filter: ['==', ['get', 'feature'], 'front'],
      layout: { visibility: vis, 'line-cap': 'round', 'line-join': 'round' },
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
    {
      id: `${idPrefix}-pips`,
      type: 'symbol',
      source: sourceId,
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

  const firstSymbolId = () => map.getStyle().layers.find((l) => l.type === 'symbol')?.id;

  // Load the pip sprites once, each baked with its front-type color. Guarded by
  // map.hasImage so a style swap (which drops user images) re-registers them.
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
      if (map.hasImage && map.hasImage(id)) continue;
      map.addImage(id, data, { sdf: false });
    }
  }

  function addLayers(specs, beforeId) {
    for (const spec of specs) {
      if (!map.getLayer(spec.id)) map.addLayer(spec, beforeId);
    }
  }

  function ensure() {
    if (!map.getSource(world.sourceId)) map.addSource(world.sourceId, world.source);
    if (!map.getSource(wpc.sourceId)) map.addSource(wpc.sourceId, wpc.source);
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

  // Re-fetch both GeoJSON documents (new analysis/cycle published).
  function reload() {
    map.getSource(FRONTS_SOURCE_ID)?.setData(wpc.dataUrl);
    map.getSource(FRONTS_WORLD_SOURCE_ID)?.setData(world.dataUrl);
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

  return { setVisible, refresh, reload, destroy };
}
