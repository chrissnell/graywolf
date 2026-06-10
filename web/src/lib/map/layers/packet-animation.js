// Packet-arrival animation layer. When a fresh RX fix is observed, draw a
// dotted green line that sweeps from the source station, through any
// H-bit-set digipeaters with a known position, to this station's "My
// Position", over ~2 seconds. After the sweep completes, the line holds
// briefly at full brightness, then fades out.
//
// Geometry matches hover-path.js exactly:
//   station.positions[0] -> each path[i] ending in '*' with a known
//   path_positions[i] -> my_position (only when station.via === 'rf'
//   and the station isn't co-located with the operator).
//
// Styling intentionally mirrors hover-path's green palette so the two
// overlays read as related; the dotted dasharray is the visual cue that
// this one is the live packet-arrival animation.
//
// Multiple animations can run concurrently. Each owns a feature in the
// shared GeoJSON sources; every animation frame rebuilds both sources
// from the active animations. The rAF loop only runs while at least one
// animation is in flight, so an idle map costs nothing.

const LINE_SRC = 'gw-packet-anim-lines';
const NODES_SRC = 'gw-packet-anim-nodes';
const LINE_LAYER = 'gw-packet-anim-line';
const NODES_LAYER = 'gw-packet-anim-nodes';

const POS_EPSILON = 0.00001;
// Sweep duration scales with on-screen pixel length so a short path
// doesn't crawl at the same wall-clock as a long one. Rate tuned so a
// ~1000 px path lands close to the previous fixed 1500 ms feel; clamped
// at both ends to keep very short flashes legible and very long sweeps
// from dragging.
const SWEEP_RATE_PX_PER_SEC = 700;
const SWEEP_MIN_MS = 350;
const SWEEP_MAX_MS = 2500;
const HOLD_MS = 600;
const FADE_MS = 500;

const EMPTY_FC = { type: 'FeatureCollection', features: [] };

function lerp(a, b, t) { return a + (b - a) * t; }

// Build the full polyline coords + intermediate digi nodes for a station.
// Returns { coords: [[lon,lat], ...], nodes: [{lon,lat}, ...] } or null
// when the line would be degenerate (single point, or source is us).
function buildPath(station, ownPos) {
  const pos = station?.positions?.[0];
  if (!pos) return null;

  // Source IS us: don't animate our own beacons echoing back through the
  // station cache. The atOwn epsilon catches GPS jitter on the local fix.
  if (ownPos) {
    const sourceIsOwn =
      Math.abs(pos.lat - ownPos.lat) < POS_EPSILON &&
      Math.abs(pos.lon - ownPos.lon) < POS_EPSILON;
    if (sourceIsOwn) return null;
  }

  const coords = [[pos.lon, pos.lat]];
  const nodes = [];
  const path = station.path || [];
  const pps = station.path_positions || [];
  for (let i = 0; i < path.length; i++) {
    if (!path[i] || !path[i].endsWith('*')) continue;
    const pp = pps[i];
    if (!Array.isArray(pp) || pp.length !== 2) continue;
    if (pp[0] === 0 && pp[1] === 0) continue;
    // path_positions entries arrive as [lat, lon]; MapLibre wants [lon, lat]
    coords.push([pp[1], pp[0]]);
    nodes.push({ lon: pp[1], lat: pp[0], callsign: path[i].replace('*', '') });
  }

  // RF stations terminate at the operator's own position; IS-gated
  // stations don't have a meaningful RF tail to draw.
  if (station.via === 'rf' && ownPos) {
    coords.push([ownPos.lon, ownPos.lat]);
  }

  if (coords.length < 2) return null;
  return { coords, nodes };
}

// Per-node arc-length fractions along the full polyline, so each digi
// dot lights up when the sweep reaches it. coords[0] is the source
// station; node n sits at coords[n+1].
function arcFractions(coords, nodeCount) {
  if (nodeCount === 0) return [];
  const segs = [];
  let total = 0;
  for (let i = 0; i < coords.length - 1; i++) {
    const dx = coords[i + 1][0] - coords[i][0];
    const dy = coords[i + 1][1] - coords[i][1];
    const len = Math.hypot(dx, dy);
    segs.push(len);
    total += len;
  }
  if (total === 0) return new Array(nodeCount).fill(1);
  const out = new Array(nodeCount);
  let acc = 0;
  for (let n = 0; n < nodeCount; n++) {
    acc += segs[n];
    out[n] = acc / total;
  }
  return out;
}

// Truncate a polyline so it ends at fractional progress p (in [0,1]) of
// its cumulative arc length. Linear interp on lon/lat directly — fine for
// a short animation overlay; we don't need geodesic correctness here.
function sweepCoords(coords, p) {
  if (p >= 1) return coords;
  if (p <= 0) return [coords[0]];

  const segs = [];
  let total = 0;
  for (let i = 0; i < coords.length - 1; i++) {
    const dx = coords[i + 1][0] - coords[i][0];
    const dy = coords[i + 1][1] - coords[i][1];
    const len = Math.hypot(dx, dy);
    segs.push(len);
    total += len;
  }
  if (total === 0) return [coords[0]];

  const target = total * p;
  const out = [coords[0]];
  let acc = 0;
  for (let i = 0; i < segs.length; i++) {
    if (acc + segs[i] >= target) {
      const segP = (target - acc) / segs[i];
      out.push([
        lerp(coords[i][0], coords[i + 1][0], segP),
        lerp(coords[i][1], coords[i + 1][1], segP),
      ]);
      return out;
    }
    out.push(coords[i + 1]);
    acc += segs[i];
  }
  return coords;
}

export function mountPacketAnimationLayer(map, getOwnPosition = () => null) {
  if (!map.getSource(LINE_SRC)) {
    map.addSource(LINE_SRC, { type: 'geojson', data: EMPTY_FC });
  }
  if (!map.getSource(NODES_SRC)) {
    map.addSource(NODES_SRC, { type: 'geojson', data: EMPTY_FC });
  }
  if (!map.getLayer(LINE_LAYER)) {
    map.addLayer({
      id: LINE_LAYER,
      type: 'line',
      source: LINE_SRC,
      layout: { 'line-cap': 'round', 'line-join': 'round' },
      paint: {
        'line-color': '#3fb950',
        'line-width': 3,
        'line-opacity': ['get', 'opacity'],
        // 1x dash + 2x gap with round caps -> short rounded dashes about
        // twice the visible length of pure round dots ([0, 2]). Increases
        // coverage so the trail reads as prominently as the solid
        // hover-path line.
        'line-dasharray': [1, 2],
      },
    });
  }
  if (!map.getLayer(NODES_LAYER)) {
    map.addLayer({
      id: NODES_LAYER,
      type: 'circle',
      source: NODES_SRC,
      paint: {
        'circle-radius': 5,
        'circle-color': '#1a1e24',
        'circle-stroke-color': '#3fb950',
        'circle-stroke-width': 2,
        'circle-opacity': ['get', 'opacity'],
        'circle-stroke-opacity': ['get', 'opacity'],
      },
    });
  }

  // Active animations: { id, coords, nodes, fracs, startedAt }
  let nextId = 1;
  const animations = [];
  let rafHandle = null;
  let visible = true;

  function tick() {
    rafHandle = null;
    const now = performance.now();
    const lineFeatures = [];
    const nodeFeatures = [];

    for (let i = animations.length - 1; i >= 0; i--) {
      const a = animations[i];
      const elapsed = now - a.startedAt;
      if (elapsed >= a.totalMs) {
        animations.splice(i, 1);
        continue;
      }

      let sweep, opacity;
      if (elapsed < a.sweepMs) {
        sweep = elapsed / a.sweepMs;
        opacity = 1;
      } else if (elapsed < a.sweepMs + HOLD_MS) {
        sweep = 1;
        opacity = 1;
      } else {
        sweep = 1;
        opacity = 1 - (elapsed - a.sweepMs - HOLD_MS) / FADE_MS;
      }

      const partial = sweepCoords(a.coords, sweep);
      lineFeatures.push({
        type: 'Feature',
        id: a.id,
        geometry: { type: 'LineString', coordinates: partial },
        properties: { opacity },
      });

      for (let n = 0; n < a.nodes.length; n++) {
        if (sweep < a.fracs[n]) continue;
        nodeFeatures.push({
          type: 'Feature',
          geometry: { type: 'Point', coordinates: [a.nodes[n].lon, a.nodes[n].lat] },
          properties: { opacity, callsign: a.nodes[n].callsign },
        });
      }
    }

    map.getSource(LINE_SRC)?.setData({ type: 'FeatureCollection', features: lineFeatures });
    map.getSource(NODES_SRC)?.setData({ type: 'FeatureCollection', features: nodeFeatures });

    if (animations.length > 0) {
      rafHandle = requestAnimationFrame(tick);
    }
  }

  function animate(station) {
    if (!visible) return;
    const built = buildPath(station, getOwnPosition());
    if (!built) return;

    // On-screen pixel length of the polyline at the current zoom. Snapshot
    // at animate-time -- if the operator zooms during the sweep, the
    // perceived rate will drift slightly, which is fine for a sub-second
    // overlay.
    let pxLen = 0;
    for (let i = 0; i < built.coords.length - 1; i++) {
      const a = map.project(built.coords[i]);
      const b = map.project(built.coords[i + 1]);
      pxLen += Math.hypot(b.x - a.x, b.y - a.y);
    }
    const rawMs = (pxLen / SWEEP_RATE_PX_PER_SEC) * 1000;
    const sweepMs = Math.max(SWEEP_MIN_MS, Math.min(SWEEP_MAX_MS, rawMs));

    animations.push({
      id: nextId++,
      coords: built.coords,
      nodes: built.nodes,
      fracs: arcFractions(built.coords, built.nodes.length),
      startedAt: performance.now(),
      sweepMs,
      totalMs: sweepMs + HOLD_MS + FADE_MS,
    });
    if (rafHandle == null) {
      rafHandle = requestAnimationFrame(tick);
    }
  }

  function clearAll() {
    animations.length = 0;
    if (rafHandle != null) {
      cancelAnimationFrame(rafHandle);
      rafHandle = null;
    }
    map.getSource(LINE_SRC)?.setData(EMPTY_FC);
    map.getSource(NODES_SRC)?.setData(EMPTY_FC);
  }

  function setVisible(v) {
    visible = v;
    const value = v ? 'visible' : 'none';
    for (const id of [LINE_LAYER, NODES_LAYER]) {
      if (map.getLayer(id)) map.setLayoutProperty(id, 'visibility', value);
    }
    if (!v) clearAll();
  }

  function destroy() {
    try {
      if (rafHandle != null) cancelAnimationFrame(rafHandle);
      rafHandle = null;
      animations.length = 0;
      for (const id of [NODES_LAYER, LINE_LAYER]) {
        if (map.getLayer(id)) map.removeLayer(id);
      }
      for (const id of [NODES_SRC, LINE_SRC]) {
        if (map.getSource(id)) map.removeSource(id);
      }
    } catch { /* map already removed */ }
  }

  return { animate, setVisible, clearAll, destroy };
}
