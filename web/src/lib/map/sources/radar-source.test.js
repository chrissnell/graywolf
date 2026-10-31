import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  DBZ_BANDS,
  DBZ_COLORS,
  RADAR_BACKEND_RASTER,
  RADAR_BACKEND_VECTOR,
  ACTIVE_RADAR_BACKEND,
  RADAR_REGION_US,
  RADAR_REGION_WORLD,
  radarTileUrl,
  radarProvider,
  radarProviderForRegion,
  worldRadarProvider,
  rainviewerTileUrl,
  rainviewerFrameTileUrl,
  rainviewerManifestUrl,
  vectorTileUrl,
  radarManifestUrl,
  radarManifestUrlForRegion,
  parseManifestFrames,
  parseRainviewerManifestFrames,
  parseManifestFramesForRegion,
} from './radar-source.js';

test('every dBZ band has a color', () => {
  assert.ok(DBZ_BANDS.length > 0);
  for (const dbz of DBZ_BANDS) {
    assert.match(DBZ_COLORS[dbz], /^#[0-9a-fA-F]{6}$/, `band ${dbz} needs a hex color`);
  }
});

test('active backend is the GRA-48 vector contours', () => {
  assert.equal(ACTIVE_RADAR_BACKEND, RADAR_BACKEND_VECTOR);
});

test('raster provider yields one raster layer driven by raster-opacity', () => {
  const p = radarProvider(RADAR_BACKEND_RASTER);
  assert.equal(p.sourceId, 'radar-tiles');
  assert.equal(p.source.type, 'raster');
  assert.equal(p.source.tileSize, 256);
  assert.match(p.source.tiles[0], /nexrad-n0q\/\{z\}\/\{x\}\/\{y\}\.png$/);
  assert.equal(p.layers.length, 1);
  assert.equal(p.layers[0].type, 'raster');
  assert.equal(p.layers[0].source, 'radar-tiles');
  assert.equal(p.opacity.property, 'raster-opacity');
  assert.deepEqual(p.opacity.layerIds, [p.layers[0].id]);
});

test('radarTileUrl builds an XYZ raster template under the base', () => {
  const url = radarTileUrl('nexrad-n0q', 'png');
  assert.ok(url.endsWith('/nexrad-n0q/{z}/{x}/{y}.png'));
});

import { buildDbzFillColor, buildRadarFillColor } from './radar-source.js';

test('buildDbzFillColor is a step expression over the dbz property', () => {
  const expr = buildDbzFillColor();
  assert.equal(expr[0], 'step');
  assert.deepEqual(expr[1], ['get', 'dbz']);
  // First element after input is the base output color, then alternating
  // stop/value pairs -- one stop per band.
  const stopCount = (expr.length - 3) / 2 + 1;
  assert.equal(stopCount, DBZ_BANDS.length);
  // Highest band maps to its palette color.
  assert.equal(expr[expr.length - 1], DBZ_COLORS[DBZ_BANDS[DBZ_BANDS.length - 1]]);
});

test('vector provider yields a fill layer driven by fill-opacity', () => {
  const p = radarProvider(RADAR_BACKEND_VECTOR);
  assert.equal(p.sourceId, 'radar-tiles');
  assert.equal(p.source.type, 'vector');
  // Per-frame provider: no static tile template -- radar.js injects it per ts.
  assert.equal(p.source.tiles, undefined);
  assert.equal(p.layers.length, 1);
  assert.equal(p.layers[0].type, 'fill');
  assert.equal(p.layers[0]['source-layer'], 'radar');
  assert.deepEqual(p.layers[0].paint['fill-color'], buildRadarFillColor());
  assert.equal(p.opacity.property, 'fill-opacity');
  assert.deepEqual(p.opacity.layerIds, ['radar-fill']);
});

test('vector source is bounded to the generated z3-z8 archive', () => {
  const p = radarProvider(RADAR_BACKEND_VECTOR);
  assert.equal(p.source.minzoom, 3);
  assert.equal(p.source.maxzoom, 8);
});

test('vectorTileUrl is the per-frame .pbf route keyed by ts (no cache-bust)', () => {
  assert.equal(vectorTileUrl(1750020000), 'https://maps.nw5w.com/radar/1750020000/{z}/{x}/{y}.pbf');
});

test('vector provider is per-frame with a frameTiles template (no cacheBust)', () => {
  const p = radarProvider(RADAR_BACKEND_VECTOR);
  assert.equal(p.perFrame, true);
  assert.equal(p.cacheBust, undefined);
  assert.equal(typeof p.frameTiles, 'function');
  assert.deepEqual(p.frameTiles(1750020000), [
    'https://maps.nw5w.com/radar/1750020000/{z}/{x}/{y}.pbf',
  ]);
});

test('radarManifestUrl points at the loop manifest', () => {
  assert.equal(radarManifestUrl(), 'https://maps.nw5w.com/radar/manifest.json');
});

test('parseManifestFrames reverses to oldest-first and keeps ts/iso', () => {
  const manifest = {
    schema_version: 1,
    frames: [
      { ts: 1750020000, iso: '2026-06-13T18:00:00Z', size: 5 }, // newest
      { ts: 1750019700, iso: '2026-06-13T17:55:00Z' },          // older
    ],
  };
  const frames = parseManifestFrames(manifest);
  assert.deepEqual(frames, [
    { ts: 1750019700, iso: '2026-06-13T17:55:00Z' },
    { ts: 1750020000, iso: '2026-06-13T18:00:00Z' },
  ]);
});

test('parseManifestFrames returns [] for bad/empty/unknown-schema input', () => {
  assert.deepEqual(parseManifestFrames(null), []);
  assert.deepEqual(parseManifestFrames({}), []);
  assert.deepEqual(parseManifestFrames({ schema_version: 2, frames: [] }), []);
  assert.deepEqual(parseManifestFrames({ schema_version: 1, frames: 'nope' }), []);
  // Drops malformed entries but keeps valid ones.
  assert.deepEqual(
    parseManifestFrames({ schema_version: 1, frames: [{ ts: 'x', iso: 'y' }, { ts: 1, iso: 'a' }] }),
    [{ ts: 1, iso: 'a' }],
  );
});

test('raster provider has no cacheBust (latest-frame IEM URL is already live)', () => {
  const p = radarProvider(RADAR_BACKEND_RASTER);
  assert.equal(p.cacheBust, undefined);
});

test('fill-color step has strictly-ascending stops and lowest-band base color', () => {
  const expr = buildDbzFillColor();
  // expr = ['step', input, base, stop1, c1, stop2, c2, ...]
  assert.equal(expr[2], DBZ_COLORS[DBZ_BANDS[0]], 'base output is the lowest band color');
  let prev = -Infinity;
  for (let i = 3; i < expr.length; i += 2) {
    const stop = expr[i];
    assert.ok(stop > prev, `stop ${stop} must exceed previous ${prev}`);
    assert.equal(expr[i + 1], DBZ_COLORS[stop], `stop ${stop} maps to its palette color`);
    prev = stop;
  }
});

test('raster source caps maxzoom so tiles overzoom instead of 404ing', () => {
  const p = radarProvider(RADAR_BACKEND_RASTER);
  assert.equal(typeof p.source.maxzoom, 'number');
  assert.ok(p.source.maxzoom >= 7 && p.source.maxzoom <= 12);
});

test('world provider is a per-frame RainViewer raster loop driven by raster-opacity', () => {
  const p = worldRadarProvider();
  assert.equal(p.sourceId, 'radar-tiles');
  assert.equal(p.source.type, 'raster');
  assert.equal(p.source.tileSize, 256);
  // Per-frame: no static `tiles` on the source (radar.js injects frameTiles(ts)).
  assert.equal(p.source.tiles, undefined);
  // RainViewer tops out at native z7; cap so MapLibre overzooms instead of
  // requesting non-existent z8+ tiles.
  assert.equal(p.source.maxzoom, 7);
  assert.equal(p.layers.length, 1);
  assert.equal(p.layers[0].type, 'raster');
  assert.equal(p.opacity.property, 'raster-opacity');
  assert.deepEqual(p.opacity.layerIds, ['radar-raster']);
});

test('world provider is per-frame with a rainviewer frameTiles template (no cacheBust)', () => {
  const p = worldRadarProvider();
  assert.equal(p.perFrame, true);
  assert.equal(p.cacheBust, undefined);
  assert.deepEqual(p.frameTiles(1700000000), [
    'https://maps.nw5w.com/radar/rainviewer/1700000000/{z}/{x}/{y}.png',
  ]);
});

test('rainviewerFrameTileUrl is the per-frame /radar/rainviewer/{ts}/ route (no query)', () => {
  const u = rainviewerFrameTileUrl(1700000000);
  assert.equal(u, 'https://maps.nw5w.com/radar/rainviewer/1700000000/{z}/{x}/{y}.png');
  assert.ok(!u.includes('?'));
});

test('rainviewerTileUrl (legacy latest) is the query-free /radar/rainviewer/ route', () => {
  assert.equal(rainviewerTileUrl(), 'https://maps.nw5w.com/radar/rainviewer/{z}/{x}/{y}.png');
  assert.ok(!rainviewerTileUrl().includes('?'));
});

test('rainviewerManifestUrl points at the RainViewer loop manifest', () => {
  assert.equal(rainviewerManifestUrl(), 'https://maps.nw5w.com/radar/rainviewer/manifest.json');
});

test('radarManifestUrlForRegion picks contour vs RainViewer manifest by region', () => {
  assert.equal(radarManifestUrlForRegion(RADAR_REGION_US), radarManifestUrl());
  assert.equal(radarManifestUrlForRegion(), radarManifestUrl());
  assert.equal(radarManifestUrlForRegion(RADAR_REGION_WORLD), rainviewerManifestUrl());
});

test('parseRainviewerManifestFrames reverses to oldest-first and synthesizes iso', () => {
  const manifest = {
    schema_version: 1,
    frames: [{ ts: 1700001200 }, { ts: 1700000600 }, { ts: 1700000000 }], // newest-first
    latest: { ts: 1700001200 },
    cadence_seconds: 600,
  };
  const frames = parseRainviewerManifestFrames(manifest);
  assert.deepEqual(
    frames.map((f) => f.ts),
    [1700000000, 1700000600, 1700001200],
  );
  // iso is synthesized from ts (the RainViewer manifest carries no iso).
  assert.equal(frames[0].iso, new Date(1700000000 * 1000).toISOString());
});

test('parseRainviewerManifestFrames returns [] for bad/empty/unknown-schema input', () => {
  assert.deepEqual(parseRainviewerManifestFrames(null), []);
  assert.deepEqual(parseRainviewerManifestFrames({}), []);
  assert.deepEqual(parseRainviewerManifestFrames({ schema_version: 2, frames: [] }), []);
  assert.deepEqual(parseRainviewerManifestFrames({ schema_version: 1, frames: 'nope' }), []);
  // Drops a frame with a non-numeric ts, keeps the good one.
  assert.deepEqual(
    parseRainviewerManifestFrames({ schema_version: 1, frames: [{ ts: 'x' }, { ts: 1 }] }).map((f) => f.ts),
    [1],
  );
});

test('parseManifestFramesForRegion dispatches to the right parser', () => {
  const us = { schema_version: 1, frames: [{ ts: 2, iso: 'b' }, { ts: 1, iso: 'a' }] };
  const world = { schema_version: 1, frames: [{ ts: 2 }, { ts: 1 }] };
  assert.deepEqual(
    parseManifestFramesForRegion(RADAR_REGION_US, us).map((f) => f.ts),
    [1, 2],
  );
  assert.deepEqual(
    parseManifestFramesForRegion(RADAR_REGION_WORLD, world).map((f) => f.ts),
    [1, 2],
  );
});

test('radarProviderForRegion: US delegates to the backend, world is per-frame RainViewer', () => {
  // US region uses the active US backend (vector contours today).
  const us = radarProviderForRegion(RADAR_REGION_US);
  assert.equal(us.source.type, radarProvider().source.type);
  assert.equal(us.perFrame, true);
  // Default region is US.
  assert.equal(radarProviderForRegion().source.type, radarProvider().source.type);
  // World region is the per-frame RainViewer raster overlay.
  const world = radarProviderForRegion(RADAR_REGION_WORLD);
  assert.equal(world.source.type, 'raster');
  assert.equal(world.perFrame, true);
  assert.match(world.frameTiles(1700000000)[0], /\/radar\/rainviewer\/1700000000\//);
});

// --- surface precipitation type palette -------------------------------------

import {
  PTYPE_BANDS,
  PTYPE_COLORS,
  PTYPE_FREEZING_RAIN,
  PTYPE_LABELS,
  PTYPE_MIXED,
  PTYPE_NONE,
  PTYPE_RAIN,
  PTYPE_SLEET,
  PTYPE_SNOW,
  buildPtypeFillColor,
} from './radar-source.js';

test('ptype codes match the generator contract and are never renumbered', () => {
  // Mirrors radar-contour/src/ptype.rs. Changing these silently remaps colors
  // on every tile already in R2.
  assert.equal(PTYPE_NONE, 0);
  assert.equal(PTYPE_RAIN, 1);
  assert.equal(PTYPE_SNOW, 2);
  assert.equal(PTYPE_SLEET, 3);
  assert.equal(PTYPE_FREEZING_RAIN, 4);
  assert.equal(PTYPE_MIXED, 5);
});

test('every frozen/mixed type has a full hex ramp', () => {
  for (const ptype of [PTYPE_SNOW, PTYPE_SLEET, PTYPE_FREEZING_RAIN, PTYPE_MIXED]) {
    const ramp = PTYPE_COLORS[ptype];
    assert.ok(ramp, `ptype ${ptype} needs a ramp`);
    for (const dbz of PTYPE_BANDS) {
      assert.match(ramp[dbz], /^#[0-9a-fA-F]{6}$/, `ptype ${ptype} band ${dbz} needs a hex color`);
    }
  }
});

test('every type has a label for the legend', () => {
  for (const ptype of [PTYPE_NONE, PTYPE_RAIN, PTYPE_SNOW, PTYPE_SLEET,
                       PTYPE_FREEZING_RAIN, PTYPE_MIXED]) {
    assert.equal(typeof PTYPE_LABELS[ptype], 'string');
    assert.ok(PTYPE_LABELS[ptype].length > 0);
  }
});

test('snow keeps the blue family and liquid gives it up', () => {
  // The whole point of the repaint: at the weak reflectivities where winter
  // precip lives, snow must be bluer than rain, or the two are indistinguishable.
  const blueness = (hex) => {
    const b = parseInt(hex.slice(5, 7), 16);
    const r = parseInt(hex.slice(1, 3), 16);
    return b - r;
  };
  for (const dbz of [10, 15, 20, 25, 30]) {
    assert.ok(blueness(PTYPE_COLORS[PTYPE_SNOW][dbz]) > 40,
      `snow at ${dbz} dBZ should read blue, got ${PTYPE_COLORS[PTYPE_SNOW][dbz]}`);
    assert.ok(blueness(DBZ_COLORS[dbz]) < 10,
      `liquid at ${dbz} dBZ must not be blue, got ${DBZ_COLORS[dbz]}`);
  }
});

// Small color helpers so the palette contract is checked numerically rather
// than by eye. Hue and chroma are what actually make two map areas tell apart;
// raw RGB distance alone is misleading near white.
const rgbOf = (hex) =>
  [0, 1, 2].map((i) => parseInt(hex.slice(1 + i * 2, 3 + i * 2), 16) / 255);
// Relative luminance. Must linearize first: a weighted sum of gamma-encoded
// channels is not luminance and understates lightness differences in the darks.
const lumOf = (hex) => {
  const [r, g, b] = rgbOf(hex).map((c) =>
    c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4);
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
};
const chromaOf = (hex) => {
  const c = rgbOf(hex);
  return Math.max(...c) - Math.min(...c);
};
const hueOf = (hex) => {
  const [r, g, b] = rgbOf(hex);
  const max = Math.max(r, g, b);
  const d = max - Math.min(r, g, b);
  if (d === 0) return 0;
  let h;
  if (max === r) h = ((g - b) / d) % 6;
  else if (max === g) h = (b - r) / d + 2;
  else h = (r - g) / d + 4;
  return (h * 60 + 360) % 360;
};
const hueGap = (a, b) => {
  const d = Math.abs(hueOf(a) - hueOf(b)) % 360;
  return Math.min(d, 360 - d);
};
const rgbGap = (a, b) => {
  const [x, y] = [rgbOf(a), rgbOf(b)];
  return (Math.abs(x[0] - y[0]) + Math.abs(x[1] - y[1]) + Math.abs(x[2] - y[2])) * 255;
};

const WINTER_TYPES = [PTYPE_SNOW, PTYPE_SLEET, PTYPE_FREEZING_RAIN, PTYPE_MIXED];
const HUED_TYPES = [PTYPE_SNOW, PTYPE_SLEET, PTYPE_FREEZING_RAIN];

test('the liquid ramp has no duplicate colors', () => {
  // Two bands shared #ffffff before this palette pass (65 and 70 dBZ), which
  // made the top of the scale unreadable. Nothing stopped that from coming back.
  const unique = new Set(Object.values(DBZ_COLORS));
  assert.equal(unique.size, DBZ_BANDS.length,
    `${DBZ_BANDS.length - unique.size} duplicate color(s) in the liquid ramp`);
});

test('the liquid ramp is brightest at its top band, not green', () => {
  // The 75 dBZ band used to jump back to #00ff00, colliding with the weak-echo
  // greens 70 dB lower down. The extreme end must read as off-the-scale.
  const top = DBZ_COLORS[DBZ_BANDS[DBZ_BANDS.length - 1]];
  assert.ok(chromaOf(top) < 0.1, `top band ${top} should be near-neutral`);
  assert.ok(lumOf(top) > 0.9, `top band ${top} should be the brightest`);
});

test('the liquid ramp leaves blue to snow at the weak bands', () => {
  // The structural fix: winter precipitation lives at 10-30 dBZ, so liquid
  // cannot also own blue there.
  for (const dbz of [5, 10, 15, 20, 25, 30]) {
    assert.ok(hueOf(DBZ_COLORS[dbz]) > 70 && hueOf(DBZ_COLORS[dbz]) < 170,
      `liquid at ${dbz} dBZ should be green, got ${DBZ_COLORS[dbz]}`);
  }
});

test('every winter ramp darkens monotonically with intensity', () => {
  // Intensity has to read within a type, not only between types.
  for (const ptype of WINTER_TYPES) {
    let prev = Infinity;
    for (const dbz of PTYPE_BANDS) {
      const l = lumOf(PTYPE_COLORS[ptype][dbz]);
      assert.ok(l < prev, `ptype ${ptype} must darken at ${dbz} dBZ (${l} vs ${prev})`);
      prev = l;
    }
  }
});

test('mixed is the only desaturated ramp, so it never reads as a hue', () => {
  // Mixed is deliberately neutral and the other three deliberately are not.
  // That chroma contrast is what keeps mixed apart from snow and freezing rain
  // even for a dichromat, for whom lightness alone would not do it.
  for (const dbz of PTYPE_BANDS) {
    assert.ok(chromaOf(PTYPE_COLORS[PTYPE_MIXED][dbz]) < 0.12,
      `mixed at ${dbz} dBZ must stay neutral`);
  }
  // Only over the bands that publish: below the 20 dBZ floor the hued ramps
  // approach white, where chroma necessarily falls away.
  for (const dbz of PTYPE_BANDS.filter((d) => d >= 20)) {
    for (const ptype of HUED_TYPES) {
      assert.ok(chromaOf(PTYPE_COLORS[ptype][dbz]) > 0.15,
        `ptype ${ptype} at ${dbz} dBZ needs enough chroma to read as a hue`);
    }
  }
});

// --- color-vision math ------------------------------------------------------
// The palette's whole correctness claim is "a reader can tell these apart",
// including readers with red-green color blindness. That is not something you
// can eyeball, so it is computed: sRGB -> linear -> LMS, collapse the lost
// channel (Vienot et al. 1999), back to sRGB, then compare in CIE L*a*b* with
// CIE76 deltaE. deltaE under ~5 is "the same color" to a human.
const toLinear = (c) => (c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4);
const linOf = (hex) => rgbOf(hex).map(toLinear);
const matMul = (m, v) => m.map((row) => row[0] * v[0] + row[1] * v[1] + row[2] * v[2]);
const RGB_TO_LMS = [
  [0.31399022, 0.63951294, 0.04649755],
  [0.15537241, 0.75789446, 0.08670142],
  [0.01775239, 0.10944209, 0.87256922],
];
const LMS_TO_RGB = [
  [5.47221206, -4.6419601, 0.16963708],
  [-1.1252419, 2.29317094, -0.1678952],
  [0.02980165, -0.19318073, 1.16364789],
];
// Deuteranopia drops the M cone; protanopia drops L. Each is reconstructed as a
// linear combination of the two the viewer retains.
const DEUTERANOPIA = [[1, 0, 0], [0.9513092, 0, 0.04866992], [0, 0, 1]];
const PROTANOPIA = [[0, 1.05118294, -0.05116099], [0, 1, 0], [0, 0, 1]];
// Returns LINEAR rgb, clamped to the displayable gamut, because `labOf` takes
// linear input. Handing it gamma-encoded values silently compresses every
// deltaE and the whole check goes soft.
const simulate = (hex, collapse) => {
  const lms = matMul(RGB_TO_LMS, linOf(hex));
  return matMul(LMS_TO_RGB, matMul(collapse, lms))
    .map((c) => Math.max(0, Math.min(1, c)));
};
const labOf = (lin) => {
  const [r, g, b] = lin;
  const xyz = [
    (0.4124 * r + 0.3576 * g + 0.1805 * b) / 0.95047,
    0.2126 * r + 0.7152 * g + 0.0722 * b,
    (0.0193 * r + 0.1192 * g + 0.9505 * b) / 1.08883,
  ];
  const f = (t) => (t > 0.008856 ? Math.cbrt(t) : 7.787 * t + 16 / 116);
  const [fx, fy, fz] = xyz.map(f);
  return [116 * fy - 16, 500 * (fx - fy), 200 * (fy - fz)];
};
const deltaE = (linA, linB) => {
  const [a, b] = [labOf(linA), labOf(linB)];
  return Math.hypot(a[0] - b[0], a[1] - b[1], a[2] - b[2]);
};

const VISION = [
  ['normal vision', (hex) => linOf(hex)],
  ['deuteranopia', (hex) => simulate(hex, DEUTERANOPIA)],
  ['protanopia', (hex) => simulate(hex, PROTANOPIA)],
];

test('no two precipitation types look alike, including to dichromats', () => {
  // This replaced a hue-separation test, which passed on a palette where snow
  // and freezing rain were 1-10 deltaE apart under simulated red-green color
  // blindness -- hue is precisely the channel those readers lose, so a hue test
  // is structurally blind to the failure it was meant to prevent. Snow vs
  // freezing rain is also the pair that matters most: one is a benign
  // inconvenience, the other brings down power lines.
  //
  // Restricted to the bands that publish (the generator runs a 20 dBZ floor);
  // below it the ramps are allowed to converge toward white.
  const published = PTYPE_BANDS.filter((d) => d >= 20);
  for (const [vision, project] of VISION) {
    for (const dbz of published) {
      for (let i = 0; i < WINTER_TYPES.length; i++) {
        for (let j = i + 1; j < WINTER_TYPES.length; j++) {
          const a = PTYPE_COLORS[WINTER_TYPES[i]][dbz];
          const b = PTYPE_COLORS[WINTER_TYPES[j]][dbz];
          const d = deltaE(project(a), project(b));
          assert.ok(d > 15,
            `ptype ${WINTER_TYPES[i]} vs ${WINTER_TYPES[j]} at ${dbz} dBZ under `
            + `${vision}: deltaE ${d.toFixed(1)} (${a} vs ${b}) -- too close to tell apart`);
        }
      }
    }
  }
});

test('the two pairs that collapse under dichromacy are split in lightness', () => {
  // The structural reason the test above passes: snow/freezing rain are both
  // blue and sleet/mixed both go near-neutral, so within each pair lightness is
  // the only channel left. Asserting it directly means a future edit that
  // re-equalizes those lightnesses fails here with a clear reason, rather than
  // failing opaquely in the deltaE sweep.
  for (const dbz of PTYPE_BANDS) {
    const gap = (a, b) => lumOf(PTYPE_COLORS[a][dbz]) - lumOf(PTYPE_COLORS[b][dbz]);
    assert.ok(gap(PTYPE_SNOW, PTYPE_FREEZING_RAIN) > 0.1,
      `snow must stay clearly lighter than freezing rain at ${dbz} dBZ`);
    assert.ok(gap(PTYPE_SLEET, PTYPE_MIXED) > 0.1,
      `sleet must stay clearly lighter than mixed at ${dbz} dBZ`);
  }
});

test('buildPtypeFillColor ramps on dbz within a type and holds above 40', () => {
  const expr = buildPtypeFillColor(PTYPE_SNOW);
  assert.equal(expr[0], 'step');
  assert.deepEqual(expr[1], ['get', 'dbz']);
  assert.equal(expr[2], PTYPE_COLORS[PTYPE_SNOW][PTYPE_BANDS[0]]);
  // No stop above the last winter band, so stronger echo keeps the darkest color.
  const stops = [];
  for (let i = 3; i < expr.length; i += 2) stops.push(expr[i]);
  assert.deepEqual(stops, PTYPE_BANDS.slice(1));
  assert.equal(expr[expr.length - 1], PTYPE_COLORS[PTYPE_SNOW][40]);
});

test('buildPtypeFillColor falls back to the liquid ramp for an unknown type', () => {
  assert.deepEqual(buildPtypeFillColor(PTYPE_RAIN), buildDbzFillColor());
  assert.deepEqual(buildPtypeFillColor(PTYPE_NONE), buildDbzFillColor());
  assert.deepEqual(buildPtypeFillColor(99), buildDbzFillColor());
});

test('buildRadarFillColor matches on ptype with liquid as the default branch', () => {
  const expr = buildRadarFillColor();
  assert.equal(expr[0], 'match');
  assert.deepEqual(expr[1], ['get', 'ptype']);
  // Trailing element is the fallback, which must be the liquid ramp so that
  // ptype 0 (unknown) and 1 (rain) both render as plain precipitation.
  assert.deepEqual(expr[expr.length - 1], buildDbzFillColor());
  // Each frozen/mixed type has its own branch with its own ramp.
  for (const ptype of [PTYPE_SNOW, PTYPE_SLEET, PTYPE_FREEZING_RAIN, PTYPE_MIXED]) {
    const at = expr.indexOf(ptype, 2);
    assert.ok(at > 0, `ptype ${ptype} needs a match branch`);
    assert.deepEqual(expr[at + 1], buildPtypeFillColor(ptype));
  }
  // Rain is NOT its own branch -- it shares the default, so an old tile with no
  // ptype attribute at all cannot fall through to nothing.
  assert.equal(expr.indexOf(PTYPE_RAIN, 2), -1);
});
