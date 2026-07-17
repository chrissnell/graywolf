<script>
  // LiveMapV2: MapLibre-based replacement for LiveMap.svelte (Leaflet).
  // Mounts the 5 map layers (stations, trails, weather, hover-path,
  // my-position), drives the data store, and renders the operator chrome:
  // the FAB + InfoPanel (layer toggles + time-range), plus the bottom
  // coord display and status bar. Cutover at task 29.

  import { onDestroy, onMount } from 'svelte';
  import maplibregl from 'maplibre-gl';
  import MaplibreMap from '../lib/map/maplibre-map.svelte';
  import InfoPanel from '../lib/map/info-panel.svelte';
  import MapContextMenu from '../lib/map/map-context-menu.svelte';
  import { COMPACT_LAYOUT_QUERY } from '../lib/compactLayout.js';
  import { createDataStore } from '../lib/map/data-store.svelte.js';
  import { mountStationsLayer } from '../lib/map/layers/stations.js';
  import { mountTrailsLayer } from '../lib/map/layers/trails.js';
  import { mountWeatherLayer } from '../lib/map/layers/weather.js';
  import { mountWindBarbsLayer } from '../lib/map/layers/wind-barbs.js';
  import { mountHoverPathLayer } from '../lib/map/layers/hover-path.js';
  import { mountMyPositionLayer } from '../lib/map/layers/my-position.js';
  import { mountRadarLayer } from '../lib/map/layers/radar.js';
  import { mountHeatmapLayer } from '../lib/map/layers/direct-rx-heatmap.js';
  import { loadHeatmap } from '../lib/map/sources/heatmap-source.js';
  import {
    radarManifestUrlForRegion,
    parseManifestFramesForRegion,
    RADAR_REGION_WORLD,
  } from '../lib/map/sources/radar-source.js';
  // Fronts layer disabled for now -- see commented-out wiring below to re-enable.
  // import { mountFrontsLayer } from '../lib/map/layers/fronts.js';
  import { frontsProvider, frontsWorldProvider } from '../lib/map/sources/fronts-source.js';
  import { createRadarFrames } from '../lib/map/radar-frames.svelte.js';
  import { mapsState } from '../lib/settings/maps-store.svelte.js';
  import { mountFixedPointsLayer } from '../lib/map/layers/fixed-points.js';
  import { fixedPointsStore } from '../lib/map/fixed-points-store.svelte.js';
  import FixedPointDialog from '../lib/map/fixed-point-dialog.svelte';
  import { renderStationPopupHTML } from '../lib/map/popup.js';
  import { unitsState } from '../lib/settings/units-store.svelte.js';
  import { mapState, MY_POSITION_ZOOM } from '../lib/map/map-store.svelte.js';
  import {
    LAYER_TOGGLES_KEY,
    parseLayerToggles,
  } from '../lib/map/layer-toggles-core.js';
  import { toMaidenhead } from '../lib/map/maidenhead.js';
  import { fmtLat, fmtLon, timeAgo } from '../lib/map/popup-helpers.js';
  import { clockOffset, formatOffsetMagnitude } from '../lib/map/clock-offset.svelte.js';
  import { directHeardWithin } from '../lib/map/direct-rx-core.js';
  import { isRfOnly } from '../lib/map/rf-only-core.js';
  import { toasts } from '../lib/stores.js';
  import { online } from '../lib/stores/connection.js';
  import MapPinPlus from 'lucide-svelte/icons/map-pin-plus';
  import MapPinned from 'lucide-svelte/icons/map-pinned';
  import Copy from 'lucide-svelte/icons/copy';

  // Values are seconds (data store wants ms; multiplied at dispatch).
  const TIMERANGES_S = [
    { value: 900, label: '15 minutes' },
    { value: 1800, label: '30 minutes' },
    { value: 3600, label: '1 hour' },
    { value: 7200, label: '2 hours' },
    { value: 14400, label: '4 hours' },
    { value: 28800, label: '8 hours' },
    { value: 43200, label: '12 hours' },
    { value: 86400, label: '1 day' },
    { value: 172800, label: '2 days' },
    { value: 345600, label: '4 days' },
    { value: 604800, label: '7 days' },
  ];

  const dataStore = createDataStore();
  // Auto-fit on first successful poll: default view → bounds of stations
  // heard in the active timerange (default 1h). One-shot, so panning/zooming
  // afterward sticks. Suppressed when the operator already has a persisted
  // view from a prior session — restoring that view is what they want, not
  // having it yanked to the station bounds on first poll.
  let didAutoFit = mapState.hasSavedView;

  // Deep-link focus: the packet log's reticle navigates here with
  // #/map?focus=CALL&lat=…&lon=… (the reverse of the popup's "APRS logs"
  // link). When present we frame the map on those coordinates on load and,
  // once the station shows up in the first poll, open its popup. lat/lon are
  // authoritative for the camera so we can fly even to a station that is older
  // than the active time-range and thus never enters the data store.
  const FOCUS_ZOOM = 12;
  const pendingFocus = parseFocusFromHash();
  let focusPopupDone = false;

  function parseFocusFromHash() {
    if (typeof window === 'undefined') return null;
    const h = window.location.hash || '';
    const qIdx = h.indexOf('?');
    if (qIdx < 0) return null;
    const params = new URLSearchParams(h.slice(qIdx + 1));
    const lat = parseFloat(params.get('lat'));
    const lon = parseFloat(params.get('lon'));
    if (!Number.isFinite(lat) || !Number.isFinite(lon)) return null;
    return { callsign: params.get('focus') || '', lat, lon };
  }

  let stationsLayer = null;
  let trailsLayer = null;
  let weatherLayer = null;
  let windBarbsLayer = null;
  let hoverPathLayer = null;
  let myPositionLayer = null;
  let radarLayer = null;
  let frontsLayer = null;
  let heatmapLayer = null;
  let heatmapTimer = null;
  let fixedPointsLayer = null;

  // Bumping this key fully remounts <MaplibreMap>, which is how we recover
  // from a permanent WebGL context loss (graywolf#461): the map component's
  // onDestroy tears down the old (context-lost, black) map and a fresh one is
  // built with a live context. Capped so a genuinely wedged GPU (every rebuild
  // immediately loses its context too) degrades to a toast instead of an
  // infinite remount loop.
  let mapGeneration = $state(0);
  let contextLossRecoveries = 0;
  let contextRecoveryResetTimer = null;
  const MAX_CONTEXT_RECOVERIES = 3;
  // A map that stays alive this long after a recovery is considered stable, so
  // the recovery budget is refilled. This distinguishes a wedged GPU (repeated
  // losses within milliseconds) from a handful of unrelated losses spread
  // across a long-running operator session, which should each recover.
  const CONTEXT_RECOVERY_STABLE_MS = 30_000;

  function handleMapContextLost() {
    if (contextLossRecoveries >= MAX_CONTEXT_RECOVERIES) {
      toasts.error(
        'The map lost its graphics context and could not recover. Reload the page to restore it.',
      );
      return;
    }
    contextLossRecoveries += 1;
    mapGeneration += 1;
  }

  // Radar overlay settings -- persisted per browser (not per account).
  const radarSettings = $state({
    visible: localStorage.getItem('gw_radar_visible') === '1',
    opacity: parseFloat(localStorage.getItem('gw_radar_opacity') ?? '0.6'),
  });
  // Coverage region ('us' = NEXRAD, 'world' = RainViewer) lives in the shared
  // map store so the maps settings tab owns the selector; the live map only
  // reflects the operator's choice here.

  // Radar loop animation. The frame store polls the worker's loop manifest and
  // drives Play/Reset + the frame slider; loadRadarFrames() fetches it with the
  // same bearer token as the basemap (a plain fetch, so transformRequest does
  // not see it -- we append ?t= here). The token is revealed once and cached.
  let radarToken = null;
  // De-dupe diagnostics: the manifest poll runs every ~15s while radar is on, so
  // a persistent failure (e.g. the Worker/generator not yet deployed) would warn
  // forever. Only log when the failure stage changes; clear on success so a
  // later regression logs again.
  let lastRadarLoadStatus = null;
  function warnRadar(status, ...args) {
    if (lastRadarLoadStatus === status) return;
    lastRadarLoadStatus = status;
    console.warn(...args);
  }
  async function loadRadarFrames() {
    if (!mapsState.registered) return [];
    if (!radarToken) radarToken = await mapsState.revealToken();
    if (!radarToken) return [];
    // Region-aware: US polls the NEXRAD contour manifest, world polls the
    // RainViewer loop manifest. Read at call time so the next poll after a
    // region switch fetches the right loop.
    const region = mapState.radarRegion;
    const isWorld = region === RADAR_REGION_WORLD;
    const url = `${radarManifestUrlForRegion(region)}?t=${encodeURIComponent(radarToken)}`;
    let resp;
    try {
      resp = await fetch(url);
    } catch (e) {
      warnRadar('network', '[radar] manifest fetch failed (network/CORS)', e);
      return [];
    }
    if (resp.status === 401) {
      radarToken = null; // stale token -- re-reveal next poll
      warnRadar(401, '[radar] manifest 401 -- token rejected; will re-reveal');
      return [];
    }
    if (resp.status === 404) {
      // The origin Worker has no manifest route for this region -- it predates
      // the animated-loop deploy. Update the worker (wrangler deploy) so the
      // overlay can load frames. Logged because the overlay otherwise sits on
      // "waiting for frames…" with no other signal.
      warnRadar(
        404,
        `[radar] manifest 404 -- origin Worker is missing the ${
          isWorld ? '/radar/rainviewer/manifest.json' : '/radar/manifest.json'
        } route. Deploy the animated-loop Worker (cd worker && npx wrangler deploy).`,
      );
      return [];
    }
    if (resp.status === 503) {
      warnRadar(
        503,
        isWorld
          ? '[radar] RainViewer manifest 503 -- Worker is up but RainViewer upstream returned no usable frames; will retry.'
          : '[radar] manifest 503 -- Worker is up but radar/manifest.json is not in R2 yet. ' +
              'Deploy/run the radar-contour generator so it publishes the manifest.',
      );
      return [];
    }
    if (!resp.ok) {
      warnRadar(resp.status, `[radar] manifest fetch HTTP ${resp.status}`);
      return [];
    }
    try {
      const frames = parseManifestFramesForRegion(region, await resp.json());
      if (frames.length === 0) warnRadar('empty', '[radar] manifest parsed but has 0 frames');
      else lastRadarLoadStatus = null; // recovered -- a later failure logs again
      return frames;
    } catch (e) {
      warnRadar('parse', '[radar] manifest JSON parse failed', e);
      return [];
    }
  }
  const radarFrames = createRadarFrames({ load: loadRadarFrames });
  // Slider label: the current frame's local time + position (e.g. "6:05 PM · 34/37").
  // No "Radar frame:" prefix -- it pushed the line to wrap, and the slider it
  // sits above already makes the context clear.
  const radarFrameLabel = $derived.by(() => {
    const f = radarFrames.current;
    if (!f) return 'Waiting for frames…';
    const t = new Date(f.ts * 1000).toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' });
    return `${t} · ${radarFrames.index + 1}/${radarFrames.count}`;
  });

  // Surface-fronts overlay. Unlike radar there is no frame loop -- one GeoJSON
  // document holds the current analysis. A slow poll (every 5 min, only while
  // the Fronts toggle is on) checks the tiny manifest and reloads the source
  // when a newer analysis is published. The manifest is a plain fetch, so we
  // append the bearer token (?t=) ourselves, mirroring loadRadarFrames(); the
  // GeoJSON data URL itself is a MapLibre source request, so transformRequest
  // attaches the token to it without any wiring here.
  let frontsToken = null;
  let frontsPollTimer = null;
  let frontsDataRetry = null; // short retry until registration+token are ready
  // One issuance marker per source (WPC analysis + model-derived world). Either
  // changing triggers a refetch of both GeoJSON documents via loadFrontsData()
  // (they share the toggle and the smoothing path).
  let lastFrontsIssued = null;
  let lastFrontsWorldIssued = null;
  // De-dupe diagnostics like warnRadar: a persistent failure would otherwise
  // warn every poll. The de-dupe is keyed by status string and tracked PER
  // SOURCE: a healthy `na` must not clear a persistently-failing `world`'s
  // warning (the normal state while the world source is rolling out). A source
  // clears only its own statuses on success.
  const frontsWarned = new Set();
  function warnFronts(status, ...args) {
    if (frontsWarned.has(status)) return;
    frontsWarned.add(status);
    console.warn(...args);
  }
  function clearFrontsWarn(label) {
    for (const s of [...frontsWarned]) if (s.startsWith(`${label}-`)) frontsWarned.delete(s);
  }
  // Fetch one fronts manifest and return its issuance marker, or undefined on
  // any failure (network/401/HTTP/parse). A 401 clears the token to re-reveal.
  async function fetchFrontsIssued(manifestUrl, label) {
    const url = `${manifestUrl}?t=${encodeURIComponent(frontsToken)}`;
    let resp;
    try {
      resp = await fetch(url);
    } catch (e) {
      warnFronts(`${label}-network`, `[fronts] ${label} manifest fetch failed (network/CORS)`, e);
      return undefined;
    }
    if (resp.status === 401) {
      frontsToken = null; // stale token -- re-reveal next poll
      warnFronts(`${label}-401`, `[fronts] ${label} manifest 401 -- token rejected; will re-reveal`);
      return undefined;
    }
    if (!resp.ok) {
      warnFronts(`${label}-${resp.status}`, `[fronts] ${label} manifest fetch HTTP ${resp.status}`);
      return undefined;
    }
    try {
      const json = await resp.json();
      const issued = json?.issued ?? json?.latest ?? null;
      clearFrontsWarn(label); // this source recovered
      return issued;
    } catch (e) {
      warnFronts(`${label}-parse`, `[fronts] ${label} manifest JSON parse failed`, e);
      return undefined;
    }
  }
  async function pollFrontsManifest() {
    if (!mapsState.registered) return;
    if (!frontsToken) frontsToken = await mapsState.revealToken();
    if (!frontsToken) return;
    const na = await fetchFrontsIssued(frontsProvider().manifestUrl, 'na');
    // If `na` got a 401 it cleared the token; don't fire a guaranteed second
    // 401 at the world manifest -- re-reveal on the next poll instead.
    if (!frontsToken) return;
    const world = await fetchFrontsIssued(frontsWorldProvider().manifestUrl, 'world');
    let changed = false;
    if (na !== undefined && na != null) {
      if (lastFrontsIssued !== null && na !== lastFrontsIssued) changed = true;
      lastFrontsIssued = na;
    }
    if (world !== undefined && world != null) {
      if (lastFrontsWorldIssued !== null && world !== lastFrontsWorldIssued) changed = true;
      lastFrontsWorldIssued = world;
    }
    if (changed) loadFrontsData();
  }
  // Fetch the GeoJSON document (token-gated, same as the manifest) and hand it
  // to the layer, which smooths the lines before rendering. Kept here rather
  // than in the layer module so all bearer-token handling stays in one place.
  // Fetch one fronts GeoJSON document (token-gated, same as the manifest) and
  // return the parsed object, or undefined on any failure. A 401 clears the
  // token to re-reveal. Diagnostics are de-duped PER SOURCE like the manifest
  // poll, so a healthy `na` never silences a persistently-failing `world`.
  async function fetchFrontsDoc(dataUrl, label) {
    const url = `${dataUrl}?t=${encodeURIComponent(frontsToken)}`;
    let resp;
    try {
      resp = await fetch(url);
    } catch (e) {
      warnFronts(`${label}-data-network`, `[fronts] ${label} geojson fetch failed (network/CORS)`, e);
      return undefined;
    }
    if (resp.status === 401) {
      frontsToken = null; // stale token -- re-reveal next load
      warnFronts(`${label}-data-401`, `[fronts] ${label} geojson 401 -- token rejected; will re-reveal`);
      return undefined;
    }
    if (!resp.ok) {
      warnFronts(`${label}-data-http`, `[fronts] ${label} geojson fetch HTTP ${resp.status}`);
      return undefined;
    }
    try {
      const json = await resp.json();
      clearFrontsWarn(label); // this source recovered
      return json;
    } catch (e) {
      warnFronts(`${label}-data-parse`, `[fronts] ${label} geojson JSON parse failed`, e);
      return undefined;
    }
  }
  // Fetch both GeoJSON documents and hand them to the layer, which smooths the
  // lines before rendering. Kept here rather than in the layer module so all
  // bearer-token handling stays in one place. The world source rolling out 404s
  // until published -- that warns once and the WPC overlay still paints.
  async function loadFrontsData() {
    if (frontsDataRetry) { clearTimeout(frontsDataRetry); frontsDataRetry = null; }
    if (!mapsState.registered) { frontsDataRetry = setTimeout(loadFrontsData, 2000); return; }
    if (!frontsToken) frontsToken = await mapsState.revealToken();
    if (!frontsToken) { frontsDataRetry = setTimeout(loadFrontsData, 2000); return; }
    const na = await fetchFrontsDoc(frontsProvider().dataUrl, 'na');
    if (na !== undefined) frontsLayer?.setData(na);
    // A 401 on the WPC doc cleared the token; skip the guaranteed second 401 and
    // re-reveal on the next load.
    if (!frontsToken) return;
    const world = await fetchFrontsDoc(frontsWorldProvider().dataUrl, 'world');
    if (world !== undefined) frontsLayer?.setWorldData(world);
  }
  function startFrontsPolling() {
    if (frontsPollTimer) return;
    loadFrontsData(); // pull the document now so the overlay paints immediately
    pollFrontsManifest(); // immediate first manifest check (drives later reloads)
    frontsPollTimer = setInterval(pollFrontsManifest, 5 * 60 * 1000);
  }
  function stopFrontsPolling() {
    if (frontsPollTimer) {
      clearInterval(frontsPollTimer);
      frontsPollTimer = null;
    }
    if (frontsDataRetry) {
      clearTimeout(frontsDataRetry);
      frontsDataRetry = null;
    }
  }

  let mapRef = null;
  let activePopup = null;

  // Operator chrome state.
  // On desktop the layer card is a perma-card (top-left); on mobile we
  // use a FAB + bottom-sheet drawer like the legacy Leaflet UI.
  let isMobile = $state(false);
  let panelOpen = $state(false);
  let layerCardCollapsed = $state(false);
  // Layer toggles are a per-browser preference (like the radar settings
  // above): persisted to localStorage so unchecking e.g. Trails or Fixed
  // Points survives navigating away and back. The load/merge logic lives in
  // layer-toggles-core.js (pure, unit-tested); here we only read/write storage.
  let layerToggles = $state(parseLayerToggles(localStorage.getItem(LAYER_TOGGLES_KEY)));

  // Add-fixed-point dialog state. Opened from the context menu with the
  // clicked coordinates; onConfirm drops the point into the store.
  let fpDialog = $state({ open: false, lat: 0, lon: 0 });

  // Direct RX predicate: a station qualifies only if it was heard directly on
  // RF (RX, zero digi hops) WITHIN the active time range. The server tracks the
  // last direct-hearing time in last_direct_heard and never advances it on a
  // later digipeated copy, so a station heard directly earlier but only via a
  // digipeater recently drops out of this filter once the direct hearing falls
  // outside the window (issue #349). Uses serverNow() so the cutoff matches the
  // host clock that stamped the timestamps.
  function isDirectRx(station) {
    const cutoffMs = clockOffset.serverNow() - dataStore.timerangeMs;
    return directHeardWithin(station, cutoffMs);
  }

  // RF Only predicate lives in rf-only-core.js: a station qualifies when its
  // current fix (positions[0]) arrived over the air (RX) and was not
  // Internet-to-RF gated. Evaluating the current fix rather than the whole
  // trail keeps the filter consistent with the marker/popup, which label a
  // station by its newest reception (graywolf #394).
  // Seed from the persisted per-browser preference so a non-default time
  // range survives reload/navigation. The $effect below pushes it into the
  // data store and writes any change back to mapState.
  let timerangeSec = $state(mapState.timerange);
  let coordText = $state('');
  let zoomLevel = $state(null);

  // Right-click context menu (background only — station markers keep
  // their own left-click popup). The menu is positioned in viewport
  // coords; the map listener supplies them on contextmenu.
  let ctxMenu = $state({ open: false, x: 0, y: 0, lat: 0, lon: 0 });
  function closeCtxMenu() {
    ctxMenu.open = false;
  }
  async function copyToClipboard(text, label) {
    try {
      await navigator.clipboard.writeText(text);
      toasts.success(`${label} copied`);
    } catch {
      toasts.error('Clipboard unavailable');
    }
  }
  // Hemispheric coords shown once in the menu header; the copy items
  // carry short labels so the menu stays narrow.
  function ctxMenuHeader() {
    return `${fmtLat(ctxMenu.lat)} ${fmtLon(ctxMenu.lon)}`;
  }
  function ctxMenuItems() {
    const { lat, lon } = ctxMenu;
    const coords = `${fmtLat(lat)} ${fmtLon(lon)}`;
    // Signed-decimal form -- the shape paste targets like Google Maps,
    // OpenStreetMap, and most spreadsheets expect. 5 decimal places is
    // ~1 m of precision, enough for any APRS use.
    const decimal = `${lat.toFixed(5)}, ${lon.toFixed(5)}`;
    const grid = toMaidenhead(lat, lon);
    return [
      {
        label: 'Add fixed beacon here',
        icon: MapPinPlus,
        primary: true,
        onSelect: () => {
          const q = `lat=${lat.toFixed(6)}&lon=${lon.toFixed(6)}`;
          window.location.hash = `#/beacons?${q}`;
        },
      },
      {
        label: 'Add fixed point here',
        icon: MapPinned,
        onSelect: () => {
          fpDialog = { open: true, lat, lon };
        },
      },
      { divider: true },
      {
        label: 'Copy coordinates',
        icon: Copy,
        onSelect: () => copyToClipboard(coords, 'Coordinates'),
      },
      {
        label: 'Copy decimal',
        icon: Copy,
        onSelect: () => copyToClipboard(decimal, 'Decimal coordinates'),
      },
      {
        label: 'Copy grid',
        icon: Copy,
        hint: grid,
        onSelect: () => copyToClipboard(grid, 'Grid square'),
      },
    ];
  }

  // Tick once a second so "5s ago" stays accurate without hammering the
  // status-bar derived state from elsewhere.
  let tickNow = $state(Date.now());
  let tickTimer = null;
  if (typeof window !== 'undefined') {
    tickTimer = window.setInterval(() => {
      tickNow = Date.now();
    }, 1000);
  }

  let mqUnsub = null;
  onMount(() => {
    if (typeof window === 'undefined') return;
    // Compact chrome (FAB + bottom-sheet) covers landscape phones too, not
    // just narrow portrait ones, so a rotated phone doesn't fall back to the
    // perma layer-card that crowds the map (GH #419).
    const mq = window.matchMedia(COMPACT_LAYOUT_QUERY);
    isMobile = mq.matches;
    const handler = (e) => (isMobile = e.matches);
    mq.addEventListener('change', handler);
    mqUnsub = () => mq.removeEventListener('change', handler);
  });

  function closePopup() {
    if (activePopup) {
      activePopup.remove();
      activePopup = null;
    }
  }

  function openStationPopup(map, station) {
    const pos = station && station.positions && station.positions[0];
    if (!pos) return;
    closePopup();

    const html = renderStationPopupHTML(station, {
      hasStation: (callsign) => dataStore.stations.has(callsign),
    });

    activePopup = new maplibregl.Popup({
      offset: 18,
      maxWidth: '320px',
      className: 'gw-station-popup',
      closeButton: true,
      closeOnClick: true,
    })
      .setLngLat([pos.lon, pos.lat])
      .setHTML(html)
      .addTo(map);

    // Keep the hover path pinned while the popup is open; clear it on close.
    hoverPathLayer?.show(station);

    activePopup.on('close', () => {
      activePopup = null;
      hoverPathLayer?.clear();
    });

    // Wire path-link clicks: pan + reopen popup for the clicked digipeater.
    const el = activePopup.getElement();
    if (el) {
      el.addEventListener('click', (ev) => {
        const link = ev.target && ev.target.closest && ev.target.closest('.path-link');
        if (!link) return;
        ev.preventDefault();
        const callsign = link.dataset.callsign;
        if (!callsign) return;
        const target = dataStore.stations.get(callsign);
        if (!target) return;
        const tpos = target.positions && target.positions[0];
        if (!tpos) return;
        map.panTo([tpos.lon, tpos.lat]);
        openStationPopup(map, target);
      });
    }
  }

  // Clicking a fixed point opens a small popup with its name and a delete
  // button — the issue's "delete on click", with a confirm step so a stray
  // tap doesn't silently drop a landmark.
  function openFixedPointPopup(map, point) {
    closePopup();
    const name = point.name || 'Fixed point';
    const div = document.createElement('div');
    div.className = 'gw-fixed-popup-body';
    const title = document.createElement('div');
    title.className = 'gw-fixed-popup-name';
    title.textContent = name;
    const coords = document.createElement('div');
    coords.className = 'gw-fixed-popup-coords';
    coords.textContent = `${fmtLat(point.lat)} ${fmtLon(point.lon)}`;
    const btn = document.createElement('button');
    btn.type = 'button';
    btn.className = 'gw-fixed-popup-delete';
    btn.textContent = 'Delete point';
    btn.addEventListener('click', async () => {
      try {
        await fixedPointsStore.remove(point.id);
        closePopup();
        toasts.success(`Removed "${name}"`);
      } catch (err) {
        toasts.error(`Could not remove point: ${err.message}`);
      }
    });
    div.append(title, coords, btn);

    activePopup = new maplibregl.Popup({
      offset: 18,
      maxWidth: '260px',
      className: 'gw-fixed-popup',
      closeButton: true,
      closeOnClick: true,
    })
      .setLngLat([point.lon, point.lat])
      .setDOMContent(div)
      .addTo(map);
    activePopup.on('close', () => {
      activePopup = null;
    });
  }

  function updateCoordText(lngLat) {
    if (!lngLat) {
      coordText = '';
      return;
    }
    const lat = lngLat.lat;
    const lon = lngLat.lng;
    coordText = `${fmtLat(lat)} ${fmtLon(lon)} · ${toMaidenhead(lat, lon)}`;
  }

  function focusStation(callsign) {
    const target = dataStore.stations.get(callsign);
    if (!target) return;
    const tpos = target.positions && target.positions[0];
    if (!tpos) return;
    mapRef?.panTo([tpos.lon, tpos.lat]);
    if (mapRef) openStationPopup(mapRef, target);
  }

  function onMapReady(map) {
    // On a context-loss remount (graywolf#461) the previous generation's
    // layers, popups and station-poll are still live; tear them down before
    // wiring the fresh map so we don't double-poll or leak DOM markers that
    // belonged to the dead map.
    if (mapRef) teardownMapGeneration();
    mapRef = map;
    // Refill the recovery budget once this map proves stable (see above). The
    // timer is cleared in teardownMapGeneration, so a map that loses its
    // context again before the window elapses does NOT get its budget back.
    contextRecoveryResetTimer = setTimeout(() => {
      contextRecoveryResetTimer = null;
      contextLossRecoveries = 0;
    }, CONTEXT_RECOVERY_STABLE_MS);
    // Radar first so the raster/fill sits below trails and station markers in
    // the GL stack. DOM layers (stations, weather) always render above the
    // canvas regardless, but GL line layers (trails) would otherwise cover it.
    radarLayer = mountRadarLayer(map, {
      visible: radarSettings.visible,
      opacity: radarSettings.opacity,
      region: mapState.radarRegion,
      // The manifest poll often resolves before the basemap style loads (tiny
      // edge-cached JSON vs a heavy basemap), so a frame ts may already be
      // known. The currentTs effect won't re-fire for an unchanged ts once the
      // layer exists, so seed the current frame here or the overlay stays blank
      // until the index changes (e.g. pressing Play).
      frameTs: radarFrames.currentTs,
      // Preload every known frame up front (one cached source each) so looping
      // toggles opacity instead of refetching tiles every cycle.
      frames: radarFrames.frames.map((f) => f.ts),
    });
    // Direct-RX heatmap sits below markers/trails in the GL stack; off by
    // default. Its data is fetched on demand (toggle/interval/pan) rather than
    // riding the station poll.
    heatmapLayer = mountHeatmapLayer(map, {
      visible: layerToggles.directRxHeatmap,
      opacity: layerToggles.directRxHeatmapOpacity,
    });
    if (layerToggles.directRxHeatmap) {
      refreshHeatmap();
      startHeatmapPolling();
    }
    // Surface fronts ride just above radar in the GL stack (lines + pip symbols
    // over the reflectivity fill) and below the station/trail markers.
    // Fronts layer disabled for now.
    // frontsLayer = mountFrontsLayer(map, { visible: layerToggles.fronts });
    // Debug hooks for live front-symbology tuning from the browser console:
    //   window.gwFronts.tune({ pipSize: 0.7, statSpacing: 30, lineWidth: 3 })
    //   window.gwFronts.info()   window.gwMap.getZoom()
    if (typeof window !== 'undefined') {
      window.gwMap = map;
      // window.gwFronts = frontsLayer;
    }
    // Trails first so the line sits beneath the (DOM) station markers
    // and below the weather labels in symbol-layer order.
    trailsLayer = mountTrailsLayer(map, () => dataStore.stations, {
      hasStation: (callsign) => dataStore.stations.has(callsign),
      focusStation,
      // hoverPathLayer is assigned just below; the callbacks fire on
      // user hover events, long after this synchronous mount block, so
      // the closure reads the populated reference.
      showHoverPath: (s) => {
        if (activePopup) return;
        hoverPathLayer?.show(s);
      },
      clearHoverPath: () => {
        if (activePopup) return;
        hoverPathLayer?.clear();
      },
    });
    // getTempSlot reaches into the station marker (assigned below) so the
    // temperature chip renders right under the callsign, not as its own
    // floating marker.
    weatherLayer = mountWeatherLayer(map, () => dataStore.stations, {
      getTempSlot: (callsign) => stationsLayer?.getTempSlot(callsign),
    });
    // Wind barbs mount before the station markers so the (DOM) station
    // icons stack above the barb staffs that radiate out from them.
    windBarbsLayer = mountWindBarbsLayer(map, () => dataStore.stations);
    hoverPathLayer = mountHoverPathLayer(map, () => {
      const my = dataStore.myPosition;
      return my ? { lat: my.lat, lon: my.lon } : null;
    });
    stationsLayer = mountStationsLayer(map, () => dataStore.stations, {
      onMarkerEnter: (s) => {
        // Don't override an open popup with a hover.
        if (activePopup) return;
        hoverPathLayer?.show(s);
      },
      onMarkerLeave: () => {
        if (activePopup) return;
        hoverPathLayer?.clear();
      },
      onMarkerClick: (s) => openStationPopup(map, s),
    });
    fixedPointsLayer = mountFixedPointsLayer(map, () => fixedPointsStore.points, {
      onMarkerClick: (point) => openFixedPointPopup(map, point),
    });
    // Render any points already in the store at mount time. On a remount
    // (e.g. navigating back to the map) the store singleton still holds the
    // last set, and the layer is created here -- after the refresh $effect's
    // first run -- so without this call pre-existing points stay invisible
    // until the next store mutation. (graywolf#347)
    fixedPointsLayer.refresh();
    // Then pull the server-side set so points placed on another device (or
    // before a browser-data wipe) show up here. load() reassigns the store
    // array, so the refresh $effect re-runs once this resolves. (graywolf#347)
    fixedPointsStore.load().catch((err) => {
      toasts.error(`Could not load fixed points: ${err.message}`);
    });
    myPositionLayer = mountMyPositionLayer(map, () => dataStore.myPosition, {
      onMarkerEnter: () => {
        if (activePopup) return;
        const my = dataStore.myPosition;
        if (!my?.callsign) return;
        const myStation = dataStore.stations.get(my.callsign);
        if (myStation) hoverPathLayer?.show(myStation);
      },
      onMarkerLeave: () => {
        if (activePopup) return;
        hoverPathLayer?.clear();
      },
    });

    // Apply the saved toggle state to the freshly-mounted layers. The
    // setVisible/setFilter effects only depend on layerToggles.*, not on the
    // layer references, so assigning a layer above does NOT re-run them. On a
    // remount with a non-default config (e.g. Trails unchecked) the layers
    // would otherwise be created visible/unfiltered and the saved preference
    // never applied until the operator toggled a checkbox. (graywolf#363)
    stationsLayer.setVisible(layerToggles.stations);
    trailsLayer.setVisible(layerToggles.trails);
    weatherLayer.setVisible(layerToggles.weather);
    windBarbsLayer.setVisible(layerToggles.weather);
    myPositionLayer.setVisible(layerToggles.myPosition);
    fixedPointsLayer.setVisible(layerToggles.fixedPoints);
    // Fronts layer disabled for now.
    // frontsLayer.setVisible(layerToggles.fronts);
    const initialPred = layerToggles.directRxOnly
      ? isDirectRx
      : layerToggles.rfOnly
        ? isRfOnly
        : null;
    stationsLayer.setFilter(initialPred);
    trailsLayer.setFilter(initialPred);
    weatherLayer.setFilter(initialPred);
    windBarbsLayer.setFilter(initialPred);

    function updateBounds() {
      const b = map.getBounds();
      dataStore.setBounds({
        swLat: b.getSouth(),
        swLon: b.getWest(),
        neLat: b.getNorth(),
        neLon: b.getEast(),
      });
    }
    map.on('moveend', updateBounds);
    updateBounds();

    // The heatmap is bbox-scoped, so a new viewport needs a fresh fetch.
    map.on('moveend', () => {
      if (layerToggles.directRxHeatmap) refreshHeatmap();
    });

    zoomLevel = map.getZoom();
    map.on('zoom', () => (zoomLevel = map.getZoom()));

    // Persist center/zoom across reloads. moveend covers pan+zoom (MapLibre
    // fires it after every camera change, including programmatic easeTo /
    // fitBounds — so the auto-fit destination is captured too).
    map.on('moveend', () => {
      const c = map.getCenter();
      mapState.mapCenter = [c.lat, c.lng];
      mapState.mapZoom = map.getZoom();
    });

    // Coord display: cheap (single $state assignment per move). MapLibre
    // already throttles mousemove to once per animation frame, so this
    // is fine without an explicit rAF gate.
    map.on('mousemove', (e) => updateCoordText(e.lngLat));
    map.on('mouseout', () => (coordText = ''));

    // Open the background context menu at viewport coords vx/vy anchored to
    // the given lngLat. Shared by right-click (desktop) and long-press
    // (touch) so both surfaces behave identically once triggered.
    function openCtxMenuAt(vx, vy, lngLat) {
      ctxMenu = { open: true, x: vx, y: vy, lat: lngLat.lat, lon: lngLat.lng };
      // Suppress hover overlays while the menu is up.
      closePopup();
      hoverPathLayer?.clear();
    }

    // Right-click background → open context menu. Bail when the click
    // landed on a marker (or any DOM child of one): station markers own a
    // left-click popup and fixed-point markers own a delete popup, so we
    // don't want the background menu to fight either surface.
    map.on('contextmenu', (e) => {
      const target = e.originalEvent?.target;
      if (target && target.closest && target.closest('.gw-station-marker, .gw-fixed-marker')) {
        return;
      }
      e.preventDefault?.();
      e.originalEvent?.preventDefault?.();
      // position:fixed menu wants viewport coords, not canvas-relative
      // (which is what e.point gives us). originalEvent.clientX/Y is the
      // right surface, with a fallback to e.point in the unlikely case
      // where the synthetic event lacks an originalEvent.
      const oe = e.originalEvent;
      const vx = oe?.clientX ?? e.point.x;
      const vy = oe?.clientY ?? e.point.y;
      openCtxMenuAt(vx, vy, e.lngLat);
    });

    // Long-press background → same context menu, for touchscreens where no
    // contextmenu event fires (graywolf#347). A single finger held still for
    // LONGPRESS_MS opens the menu; any second touch (pinch), a drag past
    // LONGPRESS_SLOP px, or an early lift cancels it so normal pan/zoom is
    // untouched. On browsers that DO synthesize a contextmenu on long-press,
    // both paths call openCtxMenuAt with near-identical coords — the second
    // open just overwrites the first single ctxMenu state, so it's a no-op,
    // not a duplicate menu.
    const LONGPRESS_MS = 500;
    const LONGPRESS_SLOP = 10;
    let lpTimer = null;
    let lpStart = null;
    function clearLongPress() {
      if (lpTimer !== null) {
        clearTimeout(lpTimer);
        lpTimer = null;
      }
      lpStart = null;
    }
    map.on('touchstart', (e) => {
      const touches = e.originalEvent?.touches;
      if (!touches || touches.length !== 1) {
        clearLongPress();
        return;
      }
      const target = e.originalEvent?.target;
      // Same guard as the right-click path: a press that lands on a marker
      // belongs to that marker's own tap surface, not the background menu.
      if (target && target.closest && target.closest('.gw-station-marker, .gw-fixed-marker')) {
        return;
      }
      const t = touches[0];
      lpStart = { x: t.clientX, y: t.clientY };
      const lngLat = e.lngLat;
      lpTimer = setTimeout(() => {
        lpTimer = null;
        openCtxMenuAt(lpStart.x, lpStart.y, lngLat);
        lpStart = null;
      }, LONGPRESS_MS);
    });
    map.on('touchmove', (e) => {
      if (!lpStart) return;
      const t = e.originalEvent?.touches?.[0];
      if (
        !t ||
        Math.abs(t.clientX - lpStart.x) > LONGPRESS_SLOP ||
        Math.abs(t.clientY - lpStart.y) > LONGPRESS_SLOP
      ) {
        clearLongPress();
      }
    });
    map.on('touchend', clearLongPress);
    map.on('touchcancel', clearLongPress);

    // Any camera change closes the menu — its anchor lat/lon would drift.
    map.on('movestart', closeCtxMenu);
    map.on('zoomstart', closeCtxMenu);

    dataStore.start();

    // Honor a #/map?focus=…&lat=…&lon= deep-link from the packet log. Claim the
    // camera before the auto-fit / my-position effects can fire (they bail when
    // didAutoFit is set) so an explicit "show this station" intent wins over the
    // default framing. The popup is opened later, once a poll has populated the
    // store (see the focus-popup effect).
    if (pendingFocus) {
      didAutoFit = true;
      map.easeTo({
        center: [pendingFocus.lon, pendingFocus.lat],
        zoom: FOCUS_ZOOM,
        duration: 600,
      });
    }
  }

  // Drive layer refresh from data-store reactivity. Touching .size
  // ensures Svelte tracks Map mutations even if the proxy short-circuits
  // a reassignment. unitsState.isMetric is read so the weather layer
  // re-renders when the operator toggles metric/imperial. tickNow is read
  // so the 1s clock drives this effect even when the station roster is
  // stable, keeping time-based layers (trail fade, staleness) current.
  // radarLayer.refresh() is idempotent here -- it only re-adds the overlay's
  // sources/layers if a basemap style swap dropped them.
  $effect(() => {
    const _size = dataStore.stations.size;
    const _isMetric = unitsState.isMetric;
    const _myPos = dataStore.myPosition; // track
    const _tick = tickNow; // 1s clock drives time-based layer refresh
    if (radarLayer) radarLayer.refresh();
    if (frontsLayer) frontsLayer.refresh();
    if (heatmapLayer) heatmapLayer.refresh();
    if (stationsLayer) stationsLayer.refresh();
    if (trailsLayer) trailsLayer.refresh();
    if (weatherLayer) weatherLayer.refresh();
    if (windBarbsLayer) windBarbsLayer.refresh();
    if (myPositionLayer) myPositionLayer.refresh();
  });

  // Fixed points change independently of the station poll (operator adds /
  // deletes them), so they get their own refresh effect tracking the store.
  // The store reassigns `points` to a new array on every load/add/remove, so
  // reading the array here makes the effect re-run on each mutation
  // (including a same-length replacement from load()).
  $effect(() => {
    const _points = fixedPointsStore.points;
    fixedPointsLayer?.refresh();
  });

  // Persist the whole toggle set on any change. JSON.stringify reads every
  // property, so Svelte tracks each one as a dependency.
  $effect(() => {
    localStorage.setItem(LAYER_TOGGLES_KEY, JSON.stringify(layerToggles));
  });
  // Push the layer toggles into the layer modules. We MUST read the
  // reactive value before the optional-chain so Svelte 5 tracks it as a
  // dependency on the initial run. With `layer?.setVisible(toggle)`, if
  // `layer` is null on first run (mount before onMapReady), the RHS is
  // short-circuited and `toggle` is never read — the effect ends up with
  // zero deps and never re-fires when the operator clicks a checkbox.
  // Reading `toggle` into a const first guarantees the dep is registered.
  $effect(() => {
    const v = layerToggles.stations;
    stationsLayer?.setVisible(v);
  });
  $effect(() => {
    const v = layerToggles.trails;
    trailsLayer?.setVisible(v);
  });
  // Wind barbs ride along with the Weather overlay -- they ARE the
  // weather wind display, so one toggle governs both the temp chip and
  // the barb rather than splitting them across two controls.
  $effect(() => {
    const v = layerToggles.weather;
    weatherLayer?.setVisible(v);
    windBarbsLayer?.setVisible(v);
  });
  $effect(() => {
    const v = layerToggles.myPosition;
    myPositionLayer?.setVisible(v);
  });
  $effect(() => {
    const v = radarSettings.visible;
    localStorage.setItem('gw_radar_visible', v ? '1' : '0');
    radarLayer?.setVisible(v);
    // Only poll the loop manifest while the overlay is on; pause playback when
    // it is hidden so the timer isn't running unseen.
    if (v) {
      radarFrames.startPolling();
    } else {
      radarFrames.pause();
      radarFrames.stopPolling();
    }
  });
  // Preload the full frame set into the layer (one cached source per frame) and
  // reconcile it as the manifest poll rolls frames in and out. Looping then
  // toggles opacity between already-loaded frames rather than refetching tiles.
  $effect(() => {
    radarLayer?.setFrames(radarFrames.frames.map((f) => f.ts));
  });
  // Drive the current animation frame into the layer. setFrameTs hands the
  // visible opacity from the previous frame to the current one (no refetch).
  $effect(() => {
    const ts = radarFrames.currentTs;
    if (ts != null) radarLayer?.setFrameTs(ts);
  });
  $effect(() => {
    const v = radarSettings.opacity;
    localStorage.setItem('gw_radar_opacity', String(v));
    radarLayer?.setOpacity(v);
  });
  // Plain (non-reactive) guard so writing it doesn't retrigger the effect; the
  // effect re-runs only when mapState.radarRegion changes.
  let radarRegionApplied = mapState.radarRegion;
  $effect(() => {
    const region = mapState.radarRegion;
    radarLayer?.setRegion(region);
    if (region !== radarRegionApplied) {
      radarRegionApplied = region;
      // The frame ts namespace changed (US contour vs RainViewer): drop the old
      // loop and immediately re-poll the new region's manifest so the slider and
      // overlay don't briefly animate the wrong region's frames.
      radarFrames.reset();
      if (radarSettings.visible) radarFrames.startPolling();
    }
  });
  $effect(() => {
    const v = layerToggles.fixedPoints;
    fixedPointsLayer?.setVisible(v);
  });
  // Surface fronts: toggle visibility, and run the slow manifest poll only while
  // the overlay is on (mirrors the radar manifest poll being gated on its
  // visibility). Reading `v` before the optional-chain registers the dep.
  // Fronts layer disabled for now.
  // $effect(() => {
  //   const v = layerToggles.fronts;
  //   frontsLayer?.setVisible(v);
  //   if (v) startFrontsPolling();
  //   else stopFrontsPolling();
  // });
  // RF reachability filter: predicate is shared across stations/trails/
  // weather/wind-barbs so the layers stay in lockstep. my-position is the
  // operator's own beacon and is intentionally exempt. Direct RX is the
  // stricter of the two (a subset of RF Only), so it wins when both are on.
  $effect(() => {
    const pred = layerToggles.directRxOnly
      ? isDirectRx
      : layerToggles.rfOnly
        ? isRfOnly
        : null;
    stationsLayer?.setFilter(pred);
    trailsLayer?.setFilter(pred);
    weatherLayer?.setFilter(pred);
    windBarbsLayer?.setFilter(pred);
  });

  // Direct-RX heatmap fetch + polling. Heat drifts slowly, so a 15s cadence
  // (vs the 5s station poll) is plenty; the fetch is bbox-scoped to the
  // current viewport and interval.
  async function refreshHeatmap() {
    if (!heatmapLayer || !mapRef) return;
    const b = mapRef.getBounds();
    const bbox = { swLat: b.getSouth(), swLon: b.getWest(), neLat: b.getNorth(), neLon: b.getEast() };
    try {
      const { geojson, maxCount } = await loadHeatmap(bbox, timerangeSec);
      heatmapLayer.refresh(geojson, maxCount);
    } catch {
      // transient fetch error; the interval retries
    }
  }

  function startHeatmapPolling() {
    stopHeatmapPolling();
    heatmapTimer = setInterval(refreshHeatmap, 15000);
  }

  function stopHeatmapPolling() {
    if (heatmapTimer) {
      clearInterval(heatmapTimer);
      heatmapTimer = null;
    }
  }

  // Toggle drives visibility + polling; opacity slider drives paint. Both keys
  // live in layerToggles, so the existing localStorage-persist effect covers
  // their persistence with no extra code.
  $effect(() => {
    const v = layerToggles.directRxHeatmap;
    heatmapLayer?.setVisible(v);
    if (v) {
      refreshHeatmap();
      startHeatmapPolling();
    } else {
      stopHeatmapPolling();
    }
  });

  $effect(() => {
    heatmapLayer?.setOpacity(layerToggles.directRxHeatmapOpacity);
  });

  $effect(() => {
    const _t = timerangeSec; // refetch when the interval changes while on
    if (layerToggles.directRxHeatmap) refreshHeatmap();
  });

  // Push the timerange into the data store and persist the selection so it
  // is restored on the next load.
  $effect(() => {
    dataStore.setTimerange(timerangeSec * 1000);
    mapState.timerange = timerangeSec;
  });

  // One-shot recenter on the station's "My Position" as soon as the data
  // store reports a valid fix. Takes precedence over fit-to-stations: the
  // operator's own location is the more useful default than a bounding
  // box of every callsign heard in the last hour. Suppressed when a
  // persisted view exists — restoring that view is what the operator
  // wants on reload.
  //
  // myPosition now refreshes on every poll tick (data-store follows live
  // GPS), so this effect re-runs continuously. The didAutoFit guard is what
  // keeps the recenter one-shot — without it the camera would snap back to
  // the blue dot on every fix and fight the operator's panning.
  $effect(() => {
    const my = dataStore.myPosition;
    if (!my || !mapRef || didAutoFit) return;
    didAutoFit = true;
    mapRef.easeTo({
      center: [my.lon, my.lat],
      zoom: MY_POSITION_ZOOM,
      duration: 600,
    });
  });

  // Fallback auto-fit: after the first poll completes, frame all stations
  // currently in the data store. Only fires when the My Position recenter
  // above did not — i.e., the station has no GPS lock and no configured
  // position. If there are also no heard stations, the map stays at the
  // world view, and didAutoFit stays false so a later myPosition arrival
  // can still claim the camera.
  $effect(() => {
    const t = dataStore.lastFetchAt;
    if (!t || !mapRef || didAutoFit) return;
    if (fitToStations()) didAutoFit = true;
  });

  // Focus deep-link popup: after the first poll completes, make one attempt to
  // open the focused station's popup. One-shot (focusPopupDone) so a station
  // heard minutes later doesn't surprise the operator with a popup; if the
  // station isn't in the store (older than the time-range), the camera fly in
  // onMapReady already framed its coordinates, which is enough.
  $effect(() => {
    const t = dataStore.lastFetchAt;
    if (!pendingFocus?.callsign || !mapRef || !t || focusPopupDone) return;
    focusPopupDone = true;
    const target = dataStore.stations.get(pendingFocus.callsign);
    if (target) openStationPopup(mapRef, target);
  });

  function fitToStations() {
    if (!mapRef) return false;
    const coords = [];
    for (const s of dataStore.stations.values()) {
      const p = s.positions && s.positions[0];
      if (p && Number.isFinite(p.lat) && Number.isFinite(p.lon)) {
        coords.push([p.lon, p.lat]);
      }
    }
    if (coords.length === 0) return false;
    if (coords.length === 1) {
      mapRef.easeTo({ center: coords[0], zoom: 9, duration: 600 });
      return true;
    }
    const bounds = new maplibregl.LngLatBounds(coords[0], coords[0]);
    for (let i = 1; i < coords.length; i++) bounds.extend(coords[i]);
    mapRef.fitBounds(bounds, { padding: 60, maxZoom: 12, duration: 600 });
    return true;
  }

  // ---- Status bar derivations ----
  let stationCount = $derived(dataStore.stations.size);
  let rfStationCount = $derived.by(() => {
    let n = 0;
    for (const s of dataStore.stations.values()) {
      if (isDirectRx(s)) n++;
    }
    return n;
  });
  let rfOnlyStationCount = $derived.by(() => {
    let n = 0;
    for (const s of dataStore.stations.values()) {
      if (isRfOnly(s)) n++;
    }
    return n;
  });
  let timerangeLabel = $derived(
    TIMERANGES_S.find((o) => o.value === timerangeSec)?.label || '',
  );
  let lastFetchAgo = $derived.by(() => {
    const t = dataStore.lastFetchAt;
    if (!t) return '';
    // Touching tickNow keeps this re-derived once a second.
    const _ = tickNow;
    // lastFetchAt is a browser-local event, so time it against the browser
    // clock — not the host-corrected serverNow().
    return timeAgo(t.toISOString(), Date.now());
  });
  // A lost connection forces the error state regardless of pollingState. When
  // the operator opens the map while already offline, the basemap style fetch
  // fails, so MaplibreMap never fires `oncreate`, dataStore.start() never runs,
  // and pollingState is stuck at its initial 'idle' — which would otherwise
  // render a misleading green dot + "idle". Reading $online directly makes the
  // status bar honest no matter how the map mounted (GH #365, #374).
  let pollDotClass = $derived(
    !$online || dataStore.pollingState === 'error'
      ? 'error'
      : dataStore.pollingState === 'polling'
        ? 'polling'
        : '',
  );
  let pollLabel = $derived(
    !$online || dataStore.pollingState === 'error'
      ? 'error'
      : dataStore.pollingState === 'polling'
        ? 'live'
        : 'idle',
  );
  // Subtle indicator: only present when the graywolf host clock differs from
  // this browser's by enough to matter (GH #234). Packet ages are already
  // corrected to the host clock; this just makes the disagreement visible.
  let clockSkew = $derived.by(() => {
    if (!clockOffset.isSignificant) return null;
    const ms = clockOffset.offsetMs;
    const mag = formatOffsetMagnitude(ms);
    return {
      text: `host clock ${ms > 0 ? '+' : '−'}${mag}`,
      title: `graywolf host clock is ${mag} ${ms > 0 ? 'ahead of' : 'behind'} this browser; packet ages are shown against the host clock.`,
    };
  });

  // Tear down everything onMapReady wires onto a specific map instance so the
  // map can be swapped (context-loss remount, graywolf#461) or fully unmounted
  // cleanly. The persistent stores/timers that are owned by the component and
  // NOT re-created by onMapReady -- the radar frame poller, fronts poller, the
  // 1s tick, the media-query listener -- outlive a generation swap and are
  // cleared in onDestroy instead.
  function teardownMapGeneration() {
    if (contextRecoveryResetTimer) {
      clearTimeout(contextRecoveryResetTimer);
      contextRecoveryResetTimer = null;
    }
    dataStore.stop();
    closePopup();
    stopHeatmapPolling();
    radarLayer?.destroy();
    frontsLayer?.destroy();
    heatmapLayer?.destroy();
    stationsLayer?.destroy();
    trailsLayer?.destroy();
    weatherLayer?.destroy();
    windBarbsLayer?.destroy();
    hoverPathLayer?.destroy();
    myPositionLayer?.destroy();
    fixedPointsLayer?.destroy();
    radarLayer = null;
    frontsLayer = null;
    heatmapLayer = null;
    stationsLayer = null;
    trailsLayer = null;
    weatherLayer = null;
    windBarbsLayer = null;
    hoverPathLayer = null;
    myPositionLayer = null;
    fixedPointsLayer = null;
    mapRef = null;
    // Drop the console debug handle so a context-lost/removed map isn't pinned
    // in the heap after a recovery remount or navigation away (graywolf#461).
    if (typeof window !== 'undefined' && window.gwMap) window.gwMap = null;
  }

  onDestroy(() => {
    teardownMapGeneration();
    radarFrames.destroy();
    stopFrontsPolling();
    if (tickTimer) {
      clearInterval(tickTimer);
      tickTimer = null;
    }
    mqUnsub?.();
    mqUnsub = null;
  });
</script>

<div class="livemap-shell">
  <!-- Start at planet view; onMapReady fits to recent stations after first poll.
       Keyed on mapGeneration so an unrecoverable WebGL context loss remounts the
       map with a fresh context instead of leaving a black canvas (graywolf#461). -->
  {#key mapGeneration}
    <MaplibreMap
      initialCenter={[mapState.mapCenter[1], mapState.mapCenter[0]]}
      initialZoom={mapState.mapZoom}
      oncreate={onMapReady}
      oncontextlost={handleMapContextLost}
    />
  {/key}

  {#snippet panelBody()}
    <!-- APRS: station/trail/position layers + the time-range filter. -->
    <section class="layer-section">
      <h3 class="layer-section-title">APRS</h3>
      <div class="layer-toggles">
        <label class="toggle-row">
          <input
            type="checkbox"
            checked={layerToggles.stations}
            onchange={(e) => (layerToggles.stations = e.currentTarget.checked)}
          />
          <span>Stations</span>
        </label>
        <label class="toggle-row">
          <input
            type="checkbox"
            checked={layerToggles.weather}
            onchange={(e) => (layerToggles.weather = e.currentTarget.checked)}
          />
          <span>Weather Stations</span>
        </label>
        <label class="toggle-row">
          <input
            type="checkbox"
            checked={layerToggles.trails}
            onchange={(e) => (layerToggles.trails = e.currentTarget.checked)}
          />
          <span>Trails</span>
        </label>
        <label class="toggle-row">
          <input
            type="checkbox"
            checked={layerToggles.myPosition}
            onchange={(e) => (layerToggles.myPosition = e.currentTarget.checked)}
          />
          <span>My Position</span>
        </label>
        <label class="toggle-row">
          <input
            type="checkbox"
            checked={layerToggles.fixedPoints}
            onchange={(e) => (layerToggles.fixedPoints = e.currentTarget.checked)}
          />
          <span>Fixed Points</span>
        </label>
        <label class="toggle-row">
          <input
            type="checkbox"
            checked={layerToggles.directRxOnly}
            onchange={(e) => (layerToggles.directRxOnly = e.currentTarget.checked)}
          />
          <span>Direct RX</span>
        </label>
        <label
          class="toggle-row"
          title="Show only stations whose plotted position was heard on RF (radio), including RF-digipeated. A station stays visible if its last fix reached us over the air, even when its most recent packet later arrived via APRS-IS."
        >
          <input
            type="checkbox"
            checked={layerToggles.rfOnly}
            onchange={(e) => (layerToggles.rfOnly = e.currentTarget.checked)}
          />
          <span>RF Only</span>
        </label>
        <label class="toggle-row">
          <input
            type="checkbox"
            checked={layerToggles.directRxHeatmap}
            onchange={(e) => (layerToggles.directRxHeatmap = e.currentTarget.checked)}
          />
          <span>RX Heatmap</span>
        </label>
      </div>

      {#if layerToggles.directRxHeatmap}
        <label class="timerange-label" for="heatmap-opacity-range">
          Heatmap opacity: {Math.round(layerToggles.directRxHeatmapOpacity * 100)}%
        </label>
        <input
          id="heatmap-opacity-range"
          type="range"
          min="0.1"
          max="1.0"
          step="0.05"
          class="radar-opacity-range"
          bind:value={layerToggles.directRxHeatmapOpacity}
        />
      {/if}

      <label class="timerange-label" for="map-timerange-select">Time range</label>
      <select
        id="map-timerange-select"
        class="map-timerange-select"
        bind:value={timerangeSec}
      >
        {#each TIMERANGES_S as opt}
          <option value={opt.value}>{opt.label}</option>
        {/each}
      </select>
    </section>

    <!-- Weather: fronts + radar overlays and their controls. (The surface-obs
         layer toggle is "Weather Stations", grouped under APRS with Stations.) -->
    <section class="layer-section">
      <h3 class="layer-section-title">Weather</h3>
      <div class="layer-toggles">
        <!-- Fronts layer disabled for now.
        <label class="toggle-row">
          <input
            type="checkbox"
            checked={layerToggles.fronts}
            onchange={(e) => (layerToggles.fronts = e.currentTarget.checked)}
          />
          <span>Fronts</span>
        </label>
        -->
        <label class="toggle-row">
          <input
            type="checkbox"
            checked={radarSettings.visible}
            onchange={(e) => (radarSettings.visible = e.currentTarget.checked)}
          />
          <span>Radar</span>
        </label>
      </div>

      <label class="timerange-label" for="radar-opacity-range">
        Radar opacity: {Math.round(radarSettings.opacity * 100)}%
      </label>
      <input
        id="radar-opacity-range"
        type="range"
        min="0.1"
        max="1.0"
        step="0.05"
        class="radar-opacity-range"
        bind:value={radarSettings.opacity}
      />

      {#if radarSettings.visible}
        <!-- Radar loop animation: two text buttons [Play/Pause][Reset] and a
             frame-position slider. Disabled until the manifest yields >1 frame. -->
        <div class="radar-anim-buttons">
          <button
            type="button"
            class="radar-anim-btn"
            onclick={() => radarFrames.toggle()}
            disabled={radarFrames.count <= 1}
            aria-label={radarFrames.playing ? 'Pause radar loop' : 'Play radar loop'}
          >
            {radarFrames.playing ? 'Pause' : 'Play'}
          </button>
          <button
            type="button"
            class="radar-anim-btn"
            onclick={() => radarFrames.stop()}
            disabled={radarFrames.count <= 1}
            aria-label="Reset radar loop and jump to the latest frame"
          >
            Reset
          </button>
        </div>
        <label class="timerange-label" for="radar-frame-range">{radarFrameLabel}</label>
        <input
          id="radar-frame-range"
          type="range"
          class="radar-frame-range"
          min="0"
          max={Math.max(0, radarFrames.count - 1)}
          step="1"
          value={radarFrames.index}
          oninput={(e) => radarFrames.seek(Number(e.currentTarget.value))}
          disabled={radarFrames.count <= 1}
        />
      {/if}
    </section>
  {/snippet}

  {#if isMobile}
    <!-- Mobile: FAB at top-left opens a bottom-sheet drawer. -->
    <button
      type="button"
      class="map-fab"
      onclick={() => (panelOpen = !panelOpen)}
      aria-label="Map controls"
      aria-expanded={panelOpen}
    >
      <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
        <polygon points="12 2 2 7 12 12 22 7 12 2" />
        <polyline points="2 17 12 22 22 17" />
        <polyline points="2 12 12 17 22 12" />
      </svg>
    </button>
    <InfoPanel title="Map Layers" bind:open={panelOpen}>
      {@render panelBody()}
    </InfoPanel>
  {:else}
    <!-- Desktop: perma-card at top-left, like the legacy Leaflet UI.
         A caret in the header collapses the body; the header stays
         visible so the operator can re-expand it. -->
    <aside
      class="layer-card"
      class:collapsed={layerCardCollapsed}
      aria-label="Map Layers"
    >
      <button
        type="button"
        class="layer-card-header"
        onclick={() => (layerCardCollapsed = !layerCardCollapsed)}
        aria-expanded={!layerCardCollapsed}
        aria-controls="layer-card-body"
      >
        <h2>Map Layers</h2>
        <svg
          class="layer-card-caret"
          width="12"
          height="12"
          viewBox="0 0 12 12"
          aria-hidden="true"
        >
          <polyline
            points="2,4 6,8 10,4"
            fill="none"
            stroke="currentColor"
            stroke-width="1.5"
            stroke-linecap="round"
            stroke-linejoin="round"
          />
        </svg>
      </button>
      {#if !layerCardCollapsed}
        <div id="layer-card-body" class="layer-card-body">
          {@render panelBody()}
        </div>
      {/if}
    </aside>
  {/if}

  <!-- Coord + zoom display (bottom-right). Zoom is always visible;
       coords/grid only appear while the cursor is over the map. -->
  {#if zoomLevel !== null || coordText}
    <div class="map-coord-display">
      {#if coordText}{coordText} &middot; {/if}z {zoomLevel?.toFixed(1) ?? ''}
    </div>
  {/if}

  <MapContextMenu
    open={ctxMenu.open}
    x={ctxMenu.x}
    y={ctxMenu.y}
    header={ctxMenu.open ? ctxMenuHeader() : ''}
    items={ctxMenu.open ? ctxMenuItems() : []}
    onclose={closeCtxMenu}
  />

  <FixedPointDialog
    bind:open={fpDialog.open}
    lat={fpDialog.lat}
    lon={fpDialog.lon}
    onConfirm={async ({ name, table, symbol, overlay, lat, lon }) => {
      try {
        const p = await fixedPointsStore.add({
          name,
          table,
          symbol,
          overlay,
          lat,
          lon,
        });
        toasts.success(`Added "${p.name}"`);
      } catch (err) {
        toasts.error(`Could not add point: ${err.message}`);
      }
    }}
  />

  <!-- Status bar (bottom-center; legacy placement so it doesn't sit
       under the sidebar on narrow desktop windows). -->
  <div class="map-status-bar" aria-live="polite">
    <span class="status-dot {pollDotClass}" aria-hidden="true"></span>
    <span>{pollLabel}</span>
    <span class="status-sep">&middot;</span>
    {#if layerToggles.directRxOnly}
      <span>{rfStationCount} heard direct / {stationCount} total</span>
    {:else if layerToggles.rfOnly}
      <span>{rfOnlyStationCount} RF reachable / {stationCount} total</span>
    {:else}
      <span>{stationCount} station{stationCount !== 1 ? 's' : ''}</span>
    {/if}
    <span class="status-sep">&middot;</span>
    <span>{timerangeLabel}</span>
    {#if lastFetchAgo}
      <span class="status-sep">&middot;</span>
      <span>{lastFetchAgo}</span>
    {/if}
    {#if clockSkew}
      <span class="status-sep">&middot;</span>
      <span class="clock-skew" title={clockSkew.title}>{clockSkew.text}</span>
    {/if}
  </div>
</div>

<style>
  .livemap-shell {
    position: absolute;
    inset: 0;
    overflow: hidden;
  }

  /* FAB (mobile only). Anchored top-LEFT so it clears MapLibre's
     NavigationControl (compass) at top-right; in a narrow portrait window
     the two used to overlap and the FAB hid the north-up reset (GH #348).
     The desktop layer-card never renders on mobile, so the left corner is
     free. */
  .map-fab {
    position: absolute;
    top: 12px;
    left: 12px;
    width: 44px;
    height: 44px;
    border-radius: 22px;
    background: var(--map-overlay-bg);
    -webkit-backdrop-filter: blur(var(--map-overlay-blur, 0));
    backdrop-filter: blur(var(--map-overlay-blur, 0));
    color: var(--map-overlay-fg);
    border: 1px solid var(--map-overlay-border);
    box-shadow: var(--map-overlay-shadow);
    cursor: pointer;
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 60;
  }
  .map-fab:hover {
    color: var(--color-text);
  }

  /* Desktop layer card (top-left, perma-visible — matches the legacy
     Leaflet UI). */
  .layer-card {
    position: absolute;
    top: 12px;
    left: 12px;
    width: 200px;
    background: var(--map-overlay-bg);
    -webkit-backdrop-filter: blur(var(--map-overlay-blur, 0));
    backdrop-filter: blur(var(--map-overlay-blur, 0));
    color: var(--map-overlay-fg);
    border: 1px solid var(--map-overlay-border);
    border-radius: 8px;
    box-shadow: var(--map-overlay-shadow);
    z-index: 50;
  }
  .layer-card-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    width: 100%;
    padding: 8px 12px;
    border: none;
    border-bottom: 1px solid var(--map-overlay-border);
    background: transparent;
    color: inherit;
    cursor: pointer;
    text-align: left;
    font: inherit;
  }
  .layer-card.collapsed .layer-card-header {
    border-bottom: none;
  }
  .layer-card-header h2 {
    margin: 0;
    font-size: 11px;
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 1px;
    color: var(--color-text-muted);
  }
  .layer-card-caret {
    color: var(--color-text-muted);
    transition: transform 120ms ease;
    flex-shrink: 0;
  }
  .layer-card.collapsed .layer-card-caret {
    transform: rotate(-90deg);
  }
  .layer-card-header:hover .layer-card-caret,
  .layer-card-header:hover h2 {
    color: var(--color-text);
  }
  .layer-card-body {
    padding: 10px 12px;
  }

  /* Grouped sections (APRS, Weather) within the layers pane. A divider +
     uppercase label separates the groups so the pane reads as two columns of
     related controls rather than one long list. */
  .layer-section + .layer-section {
    margin-top: 16px;
    padding-top: 14px;
    border-top: 1px solid var(--map-overlay-border);
  }
  .layer-section-title {
    margin: 0 0 8px;
    font-size: 11px;
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 1px;
    color: var(--color-text-muted);
  }
  .layer-toggles {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .toggle-row {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 13px;
    cursor: pointer;
    color: var(--map-overlay-fg);
  }
  .toggle-row input[type='checkbox'] {
    width: 16px;
    height: 16px;
    accent-color: var(--color-accent);
    cursor: pointer;
  }
  .timerange-label {
    display: block;
    margin-top: 14px;
    margin-bottom: 4px;
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 1px;
    color: var(--color-text-muted);
  }
  .radar-opacity-range {
    width: 100%;
    cursor: pointer;
    accent-color: var(--color-accent, #4a9eff);
  }
  /* Radar loop: two text buttons [Play/Pause][Reset] side by side. */
  .radar-anim-buttons {
    display: flex;
    gap: 6px;
    margin-top: 14px;
  }
  .radar-anim-btn {
    flex: 1;
    min-height: 32px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    padding: 6px 12px;
    background: var(--color-surface);
    color: var(--color-text);
    border: 1px solid var(--color-border);
    border-radius: 4px;
    font-size: 13px;
    cursor: pointer;
  }
  .radar-anim-btn:hover:not(:disabled) {
    border-color: var(--color-accent, #4a9eff);
    color: var(--color-accent, #4a9eff);
  }
  .radar-anim-btn:disabled {
    opacity: 0.4;
    cursor: default;
  }
  .radar-frame-range {
    width: 100%;
    cursor: pointer;
    accent-color: var(--color-accent, #4a9eff);
  }
  .radar-frame-range:disabled {
    opacity: 0.4;
    cursor: default;
  }
  .map-timerange-select {
    width: 100%;
    background: var(--color-surface);
    color: var(--color-text);
    border: 1px solid var(--color-border);
    border-radius: 4px;
    font-family: var(--font-mono);
    font-size: 13px;
    padding: 6px 8px;
    cursor: pointer;
  }
  .map-timerange-select option {
    background: var(--color-surface);
    color: var(--color-text);
  }

  /* Coord display (bottom-right; sits above MapLibre's attribution). */
  .map-coord-display {
    position: absolute;
    bottom: 28px;
    right: 12px;
    padding: 4px 10px;
    background: var(--map-overlay-bg);
    -webkit-backdrop-filter: blur(var(--map-overlay-blur, 0));
    backdrop-filter: blur(var(--map-overlay-blur, 0));
    color: var(--map-overlay-fg);
    border: 1px solid var(--map-overlay-border);
    border-radius: 4px;
    font-family: var(--font-mono);
    font-size: 12px;
    pointer-events: none;
    z-index: 40;
  }

  /* Status bar (bottom-center; matches the legacy Leaflet placement so
     it doesn't sit under the sidebar on narrow desktop windows and stays
     visible whether the sidebar is wide or collapsed). */
  .map-status-bar {
    position: absolute;
    bottom: 20px;
    left: 50%;
    transform: translateX(-50%);
    padding: 4px 10px;
    background: var(--map-overlay-bg);
    -webkit-backdrop-filter: blur(var(--map-overlay-blur, 0));
    backdrop-filter: blur(var(--map-overlay-blur, 0));
    color: var(--map-overlay-fg);
    border: 1px solid var(--map-overlay-border);
    border-radius: 4px;
    font-family: var(--font-mono);
    font-size: 12px;
    display: flex;
    gap: 6px;
    align-items: center;
    z-index: 40;
    pointer-events: none;
    white-space: nowrap;
  }
  .map-status-bar .status-dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--color-success);
  }
  .map-status-bar .status-dot.error {
    background: var(--color-danger);
  }
  .map-status-bar .status-dot.polling {
    background: var(--color-success);
  }
  .map-status-bar .status-sep {
    opacity: 0.5;
  }
  /* Clock-skew hint: present only when the host clock disagrees, so a faint
     warning tint draws a glance without shouting. */
  .map-status-bar .clock-skew {
    color: var(--color-warning, #d4a72c);
    opacity: 0.85;
  }

  /* Mobile: shrink coord/status text. Don't go below 11px to keep
     numbers readable on a small phone. Cap status-bar width and
     ellipsize so it can't push past the right edge into the coord
     display area on narrow phones. */
  @media (max-width: 480px) {
    .map-coord-display,
    .map-status-bar {
      font-size: 11px;
      padding: 4px 8px;
    }
    .map-status-bar {
      max-width: calc(100% - 24px);
      overflow: hidden;
      text-overflow: ellipsis;
    }
  }
  /* Pull the status bar and coord/zoom readout up on mobile so they clear
     the bottom safe area (home indicator / gesture bar) and the MapLibre
     attribution. The dynamic-viewport height on .main-content.full-bleed
     keeps the map's bottom edge inside the visible area as the address bar
     collapses; these offsets keep the chrome off the safe-area inset. */
  @media (max-width: 768px) {
    .map-status-bar {
      bottom: calc(14px + env(safe-area-inset-bottom));
    }
    /* The coord readout is lifted clear of this status bar in the
       max-width: 900px block below -- the two offsets are coupled; keep
       them in sync if you change either. */
  }

  /* On narrow viewports the centered status bar grows wide enough to reach
     the bottom-right coord/zoom readout and hide it (graywolf #418). Lift
     the readout clear of the status bar so the two stack instead of
     overlapping. The offset also keeps it above the bottom safe-area inset
     and MapLibre attribution on mobile. */
  @media (max-width: 900px) {
    .map-coord-display {
      bottom: calc(52px + env(safe-area-inset-bottom));
    }
  }

  /* iOS Safari auto-zooms <select> with font-size <16px on focus.
     The select lives inside the InfoPanel bottom-sheet on mobile,
     so bump it to 16px there to suppress the zoom. */
  @media (max-width: 768px) {
    .map-timerange-select {
      font-size: 16px;
    }
  }

  /* The stations layer attaches .gw-station-marker / .gw-station-label /
     .gw-station-icon elements outside this component's scope (MapLibre
     owns the DOM), so these have to be :global.

     Layout: the marker root keeps maplibregl's position:absolute (do NOT
     override with position:relative — that pulls the marker into document
     flow and the per-marker transform stacks all of them at the canvas
     origin). The 21x21 icon child is the visual anchor (anchor:'center'
     in stations.js puts the icon center on the lat/lon). The aside column
     (callsign + temperature) is absolutely positioned to the right of the
     icon and vertically centered, so its width doesn't shift the icon
     off-target. align-items:flex-end right-justifies the temp chip to the
     callsign's right edge regardless of callsign length. */
  :global(.gw-station-marker) {
    width: 21px;
    height: 21px;
    cursor: pointer;
    pointer-events: auto;
    user-select: none;
  }
  :global(.gw-station-icon) {
    width: 21px;
    height: 21px;
  }
  :global(.gw-station-aside) {
    position: absolute;
    left: 100%;
    top: 50%;
    transform: translateY(-50%);
    margin-left: 4px;
    display: flex;
    flex-direction: column;
    align-items: flex-end;
    gap: 2px;
  }
  :global(.gw-station-label) {
    padding: 0 4px;
    line-height: 12px;
    font-family: var(--font-mono);
    font-size: 10px;
    font-weight: 600;
    color: #ffffff;
    background: rgba(14, 14, 14, 0.78);
    border: 1px solid rgba(255, 255, 255, 0.6);
    border-radius: 2px;
    white-space: nowrap;
    max-width: 120px;
    overflow: hidden;
    text-overflow: ellipsis;
    box-shadow: 0 1px 2px rgba(0, 0, 0, 0.35);
  }
  /* Temperature chip: sits just below the callsign, right-justified to
     it. Filled by the weather layer. Themeable via the --map-temp-*
     tokens (defined per theme in web/themes/*.css); the fallbacks here
     are light-on-dark so it stays legible over any basemap and in night
     mode even if a theme omits them. Kept a touch dimmer than the
     callsign's pure white so the callsign stays the primary label. */
  :global(.gw-station-aside .wx-temp) {
    padding: 0 4px;
    line-height: 13px;
    font-family: var(--font-mono);
    font-size: 10px;
    font-weight: 600;
    color: var(--map-temp-fg, #e6edf3);
    background: var(--map-temp-bg, rgba(14, 14, 14, 0.82));
    border: 1px solid var(--map-temp-border, rgba(255, 255, 255, 0.45));
    border-radius: 2px;
    white-space: nowrap;
    box-shadow: 0 1px 2px rgba(0, 0, 0, 0.45);
  }

  /* Fixed-point markers: same footprint as station markers (APRS icon +
     label to the right), but the label chip is tinted to distinguish
     operator-placed landmarks from heard stations. MapLibre owns the DOM,
     so these have to be :global. */
  :global(.gw-fixed-marker) {
    width: 21px;
    height: 21px;
    cursor: pointer;
    pointer-events: auto;
    user-select: none;
  }
  :global(.gw-fixed-icon) {
    width: 21px;
    height: 21px;
  }
  :global(.gw-fixed-label) {
    position: absolute;
    left: 100%;
    top: 50%;
    transform: translateY(-50%);
    margin-left: 4px;
    padding: 0 4px;
    line-height: 12px;
    font-family: var(--font-mono);
    font-size: 10px;
    font-weight: 600;
    color: #ffffff;
    background: rgba(28, 58, 92, 0.82);
    border: 1px solid rgba(110, 181, 255, 0.7);
    border-radius: 2px;
    white-space: nowrap;
    max-width: 120px;
    overflow: hidden;
    text-overflow: ellipsis;
    box-shadow: 0 1px 2px rgba(0, 0, 0, 0.35);
  }

  /* Fixed-point delete popup: theme-aware container matching station
     popups, plus the interior name/coords/delete button. */
  :global(.gw-fixed-popup .maplibregl-popup-content) {
    background: var(--map-overlay-bg);
    -webkit-backdrop-filter: blur(var(--map-overlay-blur, 0));
    backdrop-filter: blur(var(--map-overlay-blur, 0));
    color: var(--map-overlay-fg);
    border: 1px solid var(--map-overlay-border);
    border-radius: 8px;
    box-shadow: var(--map-overlay-shadow);
    padding: 12px;
    font-size: 13px;
  }
  :global(.gw-fixed-popup .maplibregl-popup-close-button) {
    color: var(--map-overlay-fg);
    font-size: 20px;
    width: 32px;
    height: 32px;
  }
  :global(.gw-fixed-popup-body) {
    font-family: var(--font-mono);
    display: flex;
    flex-direction: column;
    gap: 6px;
    min-width: 140px;
  }
  :global(.gw-fixed-popup-name) {
    font-weight: 700;
    font-size: 13px;
    color: var(--color-text);
    padding-right: 16px;
  }
  :global(.gw-fixed-popup-coords) {
    font-size: 11px;
    color: var(--color-text-muted);
  }
  :global(.gw-fixed-popup-delete) {
    margin-top: 2px;
    padding: 5px 10px;
    border: 1px solid var(--color-danger, #d64545);
    border-radius: 4px;
    background: transparent;
    color: var(--color-danger, #d64545);
    font: inherit;
    font-size: 12px;
    cursor: pointer;
  }
  :global(.gw-fixed-popup-delete:hover) {
    background: var(--color-danger, #d64545);
    color: #ffffff;
  }

  /* Station popup: theme-aware container + tip + close button. */
  :global(.gw-station-popup .maplibregl-popup-content) {
    background: var(--map-overlay-bg);
    -webkit-backdrop-filter: blur(var(--map-overlay-blur, 0));
    backdrop-filter: blur(var(--map-overlay-blur, 0));
    color: var(--map-overlay-fg);
    border: 1px solid var(--map-overlay-border);
    border-radius: 8px;
    box-shadow: var(--map-overlay-shadow);
    padding: 12px;
    font-size: 13px;
  }
  :global(.gw-station-popup.maplibregl-popup-anchor-top .maplibregl-popup-tip) {
    border-bottom-color: var(--map-overlay-bg) !important;
  }
  :global(.gw-station-popup.maplibregl-popup-anchor-bottom .maplibregl-popup-tip) {
    border-top-color: var(--map-overlay-bg) !important;
  }
  :global(.gw-station-popup.maplibregl-popup-anchor-left .maplibregl-popup-tip) {
    border-right-color: var(--map-overlay-bg) !important;
  }
  :global(.gw-station-popup.maplibregl-popup-anchor-right .maplibregl-popup-tip) {
    border-left-color: var(--map-overlay-bg) !important;
  }
  :global(.gw-station-popup .maplibregl-popup-close-button) {
    color: var(--map-overlay-fg);
    font-size: 22px;
    width: 36px;
    height: 36px;
  }

  /* Popup interior structure. The HTML is generated by lib/map/popup.js,
     which lives outside this component, so these rules must be :global. */
  :global(.stn-popup) { font-family: var(--font-mono); }
  :global(.stn-hdr) { display: flex; align-items: center; gap: 8px; }
  :global(.stn-call) { color: #d4a040; font-size: 13px; font-weight: 700; }
  :global(.stn-sub) { color: var(--color-text-dim); font-size: 11px; margin-top: 2px; }
  /* Object/item "from CALLSIGN" line: the originating station beneath the
     object name. Sits directly under the title so source reads first,
     distinct from the relay path (.stn-via / .stn-path) shown lower down. */
  :global(.stn-src) {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 5px;
    margin-top: 3px;
    font-size: 12px;
    line-height: 1.2;
  }
  :global(.stn-src-icon) { flex: 0 0 auto; color: var(--color-text-muted); }
  :global(.stn-src-from) { color: var(--color-text-dim); }
  :global(.stn-src-call) { color: var(--color-text); font-weight: 600; }
  :global(.stn-src a.stn-src-call) { color: #6eb5ff; font-weight: 600; text-decoration: none; cursor: pointer; }
  :global(.stn-src a.stn-src-call:hover) { text-decoration: underline; }
  :global(.stn-sep) { border-top: 1px solid var(--color-border-subtle); margin: 6px 0; }
  :global(.stn-coords) { font-size: 12px; }
  :global(.stn-meta) { color: var(--color-text-muted); font-size: 12px; }
  :global(.stn-via) { font-size: 12px; margin-top: 2px; }
  :global(.via-rf) { color: var(--color-success); }
  :global(.via-rf-hops) { color: var(--color-warning); }
  :global(.via-is) { color: #c39bff; }
  /* RF-reachability note: shown when the latest packet is APRS-IS but the
     plotted fix was heard on RF (why it survives the RF Only filter). RF
     green with a help cursor to invite the explanatory tooltip. */
  :global(.stn-rf-reachable) {
    color: var(--color-success);
    font-size: 11px;
    margin-top: 2px;
    cursor: help;
  }
  :global(.stn-path) { color: var(--color-text-dim); font-size: 11px; }
  :global(.stn-path .path-link) { color: #6eb5ff; text-decoration: none; cursor: pointer; }
  :global(.stn-path .path-link:hover) { text-decoration: underline; }
  :global(.stn-comment) { color: var(--color-text-dim); font-style: italic; font-size: 12px; }
  /* Station actions: styled to match the map right-click context menu
     (.menu-item in map-context-menu.svelte) -- vertical icon+label rows
     with a hover tint rather than inline text links. Negative side margin
     lets each row's hover background extend toward the popup edges the way
     menu items do. */
  :global(.stn-actions) {
    display: flex;
    flex-direction: column;
    gap: 2px;
    margin: 2px -8px -4px;
    font-size: 13px;
  }
  :global(.stn-action) {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 4px 10px;
    border-radius: 5px;
    color: var(--map-overlay-fg);
    text-decoration: none;
    cursor: pointer;
    white-space: nowrap;
  }
  :global(.stn-action .stn-action-icon) {
    flex: 0 0 auto;
    color: var(--map-overlay-muted);
  }
  :global(.stn-action-label) { flex: 1 1 auto; }
  :global(.stn-action:hover),
  :global(.stn-action:focus-visible) {
    background: var(
      --color-surface-hover,
      color-mix(in srgb, var(--color-text) 9%, transparent)
    );
    color: var(--color-text);
    text-decoration: none;
    outline: none;
  }
  :global(.stn-weather) { font-size: 12px; }
  :global(.stn-weather-row) {
    display: flex;
    justify-content: space-between;
    gap: 12px;
    line-height: 1.4;
  }
  :global(.stn-weather-label) { color: var(--color-text-dim); }
  :global(.stn-weather-val) {
    color: var(--color-text);
    font-variant-numeric: tabular-nums;
  }

  :global(.stn-popup .badge) {
    display: inline-block;
    font-weight: 700;
    font-size: 10px;
    padding: 2px 6px;
    border-radius: 3px;
    white-space: nowrap;
  }
  :global(.stn-popup .b-rx) { background: rgba(63, 185, 80, 0.15); color: var(--color-success); }
  :global(.stn-popup .b-tx) { background: rgba(210, 153, 34, 0.15); color: var(--color-warning); }
  :global(.stn-popup .b-is) { background: rgba(195, 155, 255, 0.15); color: #c39bff; }

  /* Wind barbs -- inline SVG glyph rendered per station by
     wind-barbs.js. The marker is inert so it never steals clicks from
     the station icon underneath. Black strokes with a white halo so the
     barb stays legible over both light and dark basemaps. */
  :global(.wb-marker) {
    background: none !important;
    border: none !important;
    pointer-events: none;
    filter: drop-shadow(0 0 1px rgba(255, 255, 255, 0.95))
      drop-shadow(0 0 1.5px rgba(255, 255, 255, 0.85));
  }
  :global(.wb-svg) {
    overflow: visible;
  }
  :global(.wb-staff),
  :global(.wb-barb) {
    stroke: var(--wb-color, #111);
    stroke-width: 2;
    stroke-linecap: round;
    fill: none;
  }
  :global(.wb-pennant) {
    fill: var(--wb-color, #111);
    stroke: var(--wb-color, #111);
    stroke-width: 1;
    stroke-linejoin: round;
  }
  :global(.wb-calm) {
    fill: none;
    stroke: var(--wb-color, #111);
    stroke-width: 2;
  }

  /* Trail hover tooltip: small dim chip with the callsign, tip-less and
     non-interactive. Distinct from station popups so it doesn't pull
     theme styling for the close button etc. */
  :global(.gw-trail-tooltip .maplibregl-popup-content) {
    background: rgba(22, 27, 34, 0.85);
    color: #e0e0e0;
    border: 1px solid var(--color-border-subtle);
    border-radius: 3px;
    padding: 1px 6px;
    font-family: var(--font-mono);
    font-size: 10px;
    font-weight: 700;
    box-shadow: none;
    pointer-events: none;
  }
  :global(.gw-trail-tooltip .maplibregl-popup-tip) {
    display: none;
  }

  /* Own position marker -- iOS Maps "blue dot" look: white ring around a
     solid blue core, with a soft blue halo. The MapLibre marker DOM is
     outside this component's scope, so these have to be :global. */
  :global(.own-position-marker) {
    background: none !important;
    border: none !important;
  }
  :global(.own-position) {
    width: 16px;
    height: 16px;
    border-radius: 50%;
    background: #007aff;
    border: 2px solid #ffffff;
    box-shadow:
      0 0 0 6px rgba(0, 122, 255, 0.18),
      0 1px 4px rgba(0, 0, 0, 0.4);
  }
</style>
