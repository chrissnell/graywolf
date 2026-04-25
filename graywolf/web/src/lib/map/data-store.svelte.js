// Renderer-agnostic polling/ETag/since-cursor data layer for the live map.
//
// Wire shape (verified against pkg/webapi/stations.go):
//   GET /api/stations?bbox=sw_lat,sw_lon,ne_lat,ne_lon[&timerange=secs][&since=RFC3339Nano][&include=weather]
//   200 → []StationDTO  (sorted newest-first by last_heard); ETag header "g<gen>"
//   304 → no body; client keeps last-known data
//   401 → caller responsibility (we surface as error; the consumer redirects)
//
// Delta mode: when ?since is supplied, only stations heard at-or-after that
// timestamp are returned, and each station carries only positions[0]. The
// store reconciles these into the accumulated positions list per callsign,
// capped at MAX_TRAIL_LEN points.
//
// There is NO `removed` array in the API; expiry is driven entirely client-
// side by the active timerange. We prune any station whose last_heard is
// older than (now - timerangeMs).
//
// This module imports nothing renderer-specific. It will be consumed by
// LiveMapV2.svelte (MapLibre) once the layer modules are ported in tasks
// 20+. The legacy LiveMap.svelte (Leaflet) keeps its inline polling logic
// until cutover at task 29.

const POLL_BASE_MS = 5_000;
const POLL_MAX_MS = 60_000;
const MAX_TRAIL_LEN = 500;

function bboxToQuery(b) {
  // API parseBBox expects: sw_lat,sw_lon,ne_lat,ne_lon
  return `${b.swLat.toFixed(5)},${b.swLon.toFixed(5)},${b.neLat.toFixed(5)},${b.neLon.toFixed(5)}`;
}

function bboxEqual(a, b) {
  if (!a || !b) return false;
  return (
    a.swLat === b.swLat &&
    a.swLon === b.swLon &&
    a.neLat === b.neLat &&
    a.neLon === b.neLon
  );
}

export function createDataStore() {
  // --- Reactive collections ($state) ---
  let stations = $state(new Map());      // callsign → StationDTO (positions accumulated)
  let trails = $state(new Map());        // callsign → StationPosDTO[] (newest-first)
  let weather = $state(new Map());       // callsign → WeatherDTO
  let myPosition = $state(null);         // PositionDTO | null
  let lastFetchAt = $state(null);        // Date | null
  let pollingState = $state('idle');     // 'idle' | 'polling' | 'error'
  let timerangeMs = $state(3_600_000);   // default 1 hour

  // --- Non-reactive internals ---
  let bbox = null;                       // { swLat, swLon, neLat, neLon } | null
  let etag = null;                       // last response ETag, used in If-None-Match
  let sinceCursor = null;                // RFC3339Nano string of newest last_heard seen
  let timer = null;                      // setTimeout handle
  let inFlight = false;                  // dedupe overlapping fetches
  let backoff = POLL_BASE_MS;            // current delay; doubles on error
  let started = false;                   // start()/stop() guard
  let visibilityHandler = null;          // bound listener for cleanup

  // --- Reset cursors on bounds/timerange change so next poll does a full reload ---
  function invalidate() {
    etag = null;
    sinceCursor = null;
  }

  function pruneStale() {
    const cutoff = Date.now() - timerangeMs;
    for (const [callsign, s] of stations) {
      const heard = new Date(s.last_heard).getTime();
      if (heard < cutoff) {
        stations.delete(callsign);
        trails.delete(callsign);
        weather.delete(callsign);
      }
    }
    // Reassign so $state notices map mutations (Svelte's reactive Map proxies
    // mutations directly, but the explicit reassign keeps consumers safe even
    // if a future Svelte version drops the proxy).
    stations = stations;
    trails = trails;
    weather = weather;
  }

  function mergeStation(incoming, isDelta) {
    const existing = stations.get(incoming.callsign);

    // First fix or full reload: take the DTO as-is (positions array authoritative).
    if (!existing || !isDelta) {
      stations.set(incoming.callsign, incoming);
      trails.set(
        incoming.callsign,
        Array.isArray(incoming.positions) ? incoming.positions.slice() : [],
      );
      if (incoming.weather) {
        weather.set(incoming.callsign, incoming.weather);
      } else if (!existing) {
        // new station with no weather — nothing to do
      }
      return;
    }

    // Delta merge: incoming.positions holds exactly one element (the newest
    // fix). Prepend to the existing trail unless it's a duplicate timestamp.
    const newPos = incoming.positions && incoming.positions[0];
    if (newPos) {
      const oldTrail = trails.get(incoming.callsign) || [];
      const dup =
        oldTrail.length > 0 &&
        oldTrail[0].timestamp === newPos.timestamp &&
        oldTrail[0].lat === newPos.lat &&
        oldTrail[0].lon === newPos.lon;
      const merged = dup
        ? oldTrail
        : [newPos, ...oldTrail].slice(0, MAX_TRAIL_LEN);
      trails.set(incoming.callsign, merged);

      // Update the station summary with the new top-level fields, but keep
      // the accumulated positions array under positions for downstream
      // consumers that expect StationDTO shape.
      const merged_station = {
        ...existing,
        ...incoming,
        positions: merged,
      };
      stations.set(incoming.callsign, merged_station);
    }

    if (incoming.weather) {
      weather.set(incoming.callsign, incoming.weather);
    }
  }

  async function fetchOnce() {
    if (!bbox) return;            // bounds not set yet
    if (inFlight) return;
    inFlight = true;

    try {
      const params = new URLSearchParams();
      params.set('bbox', bboxToQuery(bbox));
      params.set('timerange', String(Math.floor(timerangeMs / 1000)));
      params.set('include', 'weather');
      if (sinceCursor) params.set('since', sinceCursor);

      const headers = {};
      if (etag) headers['If-None-Match'] = etag;

      const res = await fetch(`/api/stations?${params.toString()}`, {
        credentials: 'same-origin',
        headers,
      });

      if (res.status === 304) {
        backoff = POLL_BASE_MS;
        pollingState = 'polling';
        lastFetchAt = new Date();
        return;
      }
      if (res.status === 401) {
        pollingState = 'error';
        throw new Error('Unauthorized');
      }
      if (!res.ok) {
        throw new Error(`HTTP ${res.status}`);
      }

      const isDelta = sinceCursor !== null;
      const body = await res.json();
      etag = res.headers.get('ETag');

      // Server returns newest-first; iterate forward so the merged-station
      // ends up consistent regardless of order.
      if (Array.isArray(body)) {
        for (const s of body) {
          mergeStation(s, isDelta);
        }
        if (body.length > 0) {
          // body[0] is newest; advance cursor to its last_heard.
          sinceCursor = body[0].last_heard;
        }
      }

      pruneStale();

      backoff = POLL_BASE_MS;
      pollingState = 'polling';
      lastFetchAt = new Date();
    } catch (e) {
      console.error('[data-store] poll error:', e);
      backoff = Math.min(backoff * 2, POLL_MAX_MS);
      pollingState = 'error';
    } finally {
      inFlight = false;
    }
  }

  async function fetchMyPosition() {
    try {
      const res = await fetch('/api/position', { credentials: 'same-origin' });
      if (!res.ok) return;
      const pos = await res.json();
      myPosition = pos && pos.valid ? pos : null;
    } catch (_) {
      // Non-fatal; leave myPosition unchanged.
    }
  }

  function schedule() {
    if (typeof window === 'undefined') return;
    clearTimeout(timer);
    timer = setTimeout(async () => {
      if (!started) return;
      // Visibility-aware: skip the network call but keep the timer alive so
      // we resume promptly when the tab becomes visible again.
      if (typeof document !== 'undefined' && document.visibilityState !== 'visible') {
        schedule();
        return;
      }
      await fetchOnce();
      if (started) schedule();
    }, backoff);
  }

  function onVisibility() {
    if (typeof document === 'undefined') return;
    if (document.visibilityState === 'visible' && started) {
      // Immediate catch-up; clearTimeout inside schedule() prevents double-chains.
      fetchOnce().then(() => {
        if (started) schedule();
      });
    }
  }

  // --- Imperative API ---

  function setBounds(next) {
    // next: { swLat, swLon, neLat, neLon }
    if (bboxEqual(bbox, next)) return;
    bbox = next;
    invalidate();
    if (started) {
      // Force an immediate refresh on bounds change.
      clearTimeout(timer);
      fetchOnce().then(() => {
        if (started) schedule();
      });
    }
  }

  function setTimerange(ms) {
    if (typeof ms !== 'number' || ms <= 0 || ms === timerangeMs) return;
    timerangeMs = ms;
    invalidate();
    pruneStale();
    if (started) {
      clearTimeout(timer);
      fetchOnce().then(() => {
        if (started) schedule();
      });
    }
  }

  function start() {
    if (started) return;
    started = true;
    backoff = POLL_BASE_MS;
    pollingState = 'polling';

    if (typeof document !== 'undefined') {
      visibilityHandler = onVisibility;
      document.addEventListener('visibilitychange', visibilityHandler);
    }

    fetchMyPosition();
    // Fire one immediately so the UI populates without waiting for the timer.
    fetchOnce().then(() => {
      if (started) schedule();
    });
  }

  function stop() {
    started = false;
    pollingState = 'idle';
    if (timer) {
      clearTimeout(timer);
      timer = null;
    }
    if (visibilityHandler && typeof document !== 'undefined') {
      document.removeEventListener('visibilitychange', visibilityHandler);
      visibilityHandler = null;
    }
    inFlight = false;
  }

  return {
    // Reactive getters (read-only from consumers).
    get stations() { return stations; },
    get trails() { return trails; },
    get weather() { return weather; },
    get myPosition() { return myPosition; },
    get lastFetchAt() { return lastFetchAt; },
    get pollingState() { return pollingState; },
    get timerangeMs() { return timerangeMs; },

    // Imperative controls.
    setBounds,
    setTimerange,
    start,
    stop,
  };
}
