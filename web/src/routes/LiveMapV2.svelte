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
  import { createDataStore } from '../lib/map/data-store.svelte.js';
  import { mountStationsLayer } from '../lib/map/layers/stations.js';
  import { mountTrailsLayer } from '../lib/map/layers/trails.js';
  import { mountWeatherLayer } from '../lib/map/layers/weather.js';
  import { mountWindBarbsLayer } from '../lib/map/layers/wind-barbs.js';
  import { mountHoverPathLayer } from '../lib/map/layers/hover-path.js';
  import { mountMyPositionLayer } from '../lib/map/layers/my-position.js';
  import { mountRadarLayer } from '../lib/map/layers/radar.js';
  import {
    radarManifestUrlForRegion,
    parseManifestFramesForRegion,
    RADAR_REGION_WORLD,
  } from '../lib/map/sources/radar-source.js';
  import { createRadarFrames } from '../lib/map/radar-frames.svelte.js';
  import { mapsState } from '../lib/settings/maps-store.svelte.js';
  import { mountFixedPointsLayer } from '../lib/map/layers/fixed-points.js';
  import { fixedPointsStore } from '../lib/map/fixed-points-store.svelte.js';
  import FixedPointDialog from '../lib/map/fixed-point-dialog.svelte';
  import { renderStationPopupHTML } from '../lib/map/popup.js';
  import { unitsState } from '../lib/settings/units-store.svelte.js';
  import { mapState, MY_POSITION_ZOOM } from '../lib/map/map-store.svelte.js';
  import { toMaidenhead } from '../lib/map/maidenhead.js';
  import { fmtLat, fmtLon, timeAgo } from '../lib/map/popup-helpers.js';
  import { clockOffset, formatOffsetMagnitude } from '../lib/map/clock-offset.svelte.js';
  import { toasts } from '../lib/stores.js';
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
  let stationsLayer = null;
  let trailsLayer = null;
  let weatherLayer = null;
  let windBarbsLayer = null;
  let hoverPathLayer = null;
  let myPositionLayer = null;
  let radarLayer = null;
  let fixedPointsLayer = null;

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

  let mapRef = null;
  let activePopup = null;

  // Operator chrome state.
  // On desktop the layer card is a perma-card (top-left); on mobile we
  // use a FAB + bottom-sheet drawer like the legacy Leaflet UI.
  let isMobile = $state(false);
  let panelOpen = $state(false);
  let layerCardCollapsed = $state(false);
  let layerToggles = $state({
    stations: true,
    trails: true,
    weather: true,
    myPosition: true,
    fixedPoints: true,
    directRxOnly: false,
    rfOnly: false,
  });

  // Add-fixed-point dialog state. Opened from the context menu with the
  // clicked coordinates; onConfirm drops the point into the store.
  let fpDialog = $state({ open: false, lat: 0, lon: 0 });

  // Direct RX predicate: a station qualifies if at least one of its
  // accumulated positions arrived directly on RF (RX direction with
  // zero digi hops). Anything iGated (IS) or digipeated is excluded.
  function isDirectRx(station) {
    const pts = station?.positions;
    if (!Array.isArray(pts) || pts.length === 0) return false;
    for (const p of pts) {
      if (p.direction === 'RX' && (p.hops ?? 0) === 0) return true;
    }
    return false;
  }

  // RF Only predicate: a station qualifies if at least one position was
  // heard over the air (RX) and did not arrive via Internet-to-RF gating
  // (the `gated` flag, set on the inner packet of a third-party gate).
  // Unlike Direct RX this keeps RF-digipeated stations; it only drops
  // points that are merely Internet traffic some iGate pushed onto RF.
  function isRfOnly(station) {
    const pts = station?.positions;
    if (!Array.isArray(pts) || pts.length === 0) return false;
    for (const p of pts) {
      if (p.direction === 'RX' && !p.gated) return true;
    }
    return false;
  }
  let timerangeSec = $state(Math.floor(dataStore.timerangeMs / 1000));
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
    const mq = window.matchMedia('(max-width: 768px)');
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
    btn.addEventListener('click', () => {
      fixedPointsStore.remove(point.id);
      closePopup();
      toasts.success(`Removed "${name}"`);
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
    mapRef = map;
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

    // Right-click background → open context menu. Bail when the click
    // landed on a station marker (or any DOM child of one): markers own
    // a left-click popup and we don't want to fight that surface.
    map.on('contextmenu', (e) => {
      const target = e.originalEvent?.target;
      if (target && target.closest && target.closest('.gw-station-marker')) {
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
      ctxMenu = {
        open: true,
        x: vx,
        y: vy,
        lat: e.lngLat.lat,
        lon: e.lngLat.lng,
      };
      // Suppress hover overlays while the menu is up.
      closePopup();
      hoverPathLayer?.clear();
    });
    // Any camera change closes the menu — its anchor lat/lon would drift.
    map.on('movestart', closeCtxMenu);
    map.on('zoomstart', closeCtxMenu);

    dataStore.start();
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
    if (stationsLayer) stationsLayer.refresh();
    if (trailsLayer) trailsLayer.refresh();
    if (weatherLayer) weatherLayer.refresh();
    if (windBarbsLayer) windBarbsLayer.refresh();
    if (myPositionLayer) myPositionLayer.refresh();
  });

  // Fixed points change independently of the station poll (operator adds /
  // deletes them), so they get their own refresh effect tracking the store.
  $effect(() => {
    const _len = fixedPointsStore.points.length;
    fixedPointsLayer?.refresh();
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

  // Push the timerange into the data store.
  $effect(() => {
    dataStore.setTimerange(timerangeSec * 1000);
  });

  // One-shot recenter on the station's "My Position" as soon as the data
  // store reports a valid fix. Takes precedence over fit-to-stations: the
  // operator's own location is the more useful default than a bounding
  // box of every callsign heard in the last hour. Suppressed when a
  // persisted view exists — restoring that view is what the operator
  // wants on reload.
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
  let pollDotClass = $derived(
    dataStore.pollingState === 'error'
      ? 'error'
      : dataStore.pollingState === 'polling'
        ? 'polling'
        : '',
  );
  let pollLabel = $derived(
    dataStore.pollingState === 'error'
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

  onDestroy(() => {
    dataStore.stop();
    closePopup();
    radarFrames.destroy();
    radarLayer?.destroy();
    stationsLayer?.destroy();
    trailsLayer?.destroy();
    weatherLayer?.destroy();
    windBarbsLayer?.destroy();
    hoverPathLayer?.destroy();
    myPositionLayer?.destroy();
    fixedPointsLayer?.destroy();
    radarLayer = null;
    stationsLayer = null;
    trailsLayer = null;
    weatherLayer = null;
    windBarbsLayer = null;
    hoverPathLayer = null;
    myPositionLayer = null;
    fixedPointsLayer = null;
    mapRef = null;
    if (tickTimer) {
      clearInterval(tickTimer);
      tickTimer = null;
    }
    mqUnsub?.();
    mqUnsub = null;
  });
</script>

<div class="livemap-shell">
  <!-- Start at planet view; onMapReady fits to recent stations after first poll. -->
  <MaplibreMap
    initialCenter={[mapState.mapCenter[1], mapState.mapCenter[0]]}
    initialZoom={mapState.mapZoom}
    oncreate={onMapReady}
  />

  {#snippet panelBody()}
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
          checked={layerToggles.trails}
          onchange={(e) => (layerToggles.trails = e.currentTarget.checked)}
        />
        <span>Trails</span>
      </label>
      <label class="toggle-row">
        <input
          type="checkbox"
          checked={layerToggles.weather}
          onchange={(e) => (layerToggles.weather = e.currentTarget.checked)}
        />
        <span>Weather</span>
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
      <label class="toggle-row">
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
  {/snippet}

  {#if isMobile}
    <!-- Mobile: FAB at top-right opens a bottom-sheet drawer. -->
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
    onConfirm={({ name, table, symbol, overlay }) => {
      const p = fixedPointsStore.add({
        name,
        table,
        symbol,
        overlay,
        lat: fpDialog.lat,
        lon: fpDialog.lon,
      });
      toasts.success(`Added "${p.name}"`);
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

  /* FAB (mobile only). Sits to the LEFT of MapLibre's NavigationControl
     (which is already top-right at the default 10px inset). NavigationControl
     itself is hidden on touch widths via maplibre-map.svelte's media query,
     so the FAB has the corner to itself. */
  .map-fab {
    position: absolute;
    top: 12px;
    right: 12px;
    width: 44px;
    height: 44px;
    border-radius: 22px;
    background: var(--map-overlay-bg);
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
  /* Pull the status bar up on mobile so it clears the bottom safe area
     and the MapLibre attribution. */
  @media (max-width: 768px) {
    .map-status-bar {
      bottom: 14px;
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
  :global(.stn-sep) { border-top: 1px solid var(--color-border-subtle); margin: 6px 0; }
  :global(.stn-coords) { font-size: 12px; }
  :global(.stn-meta) { color: var(--color-text-muted); font-size: 12px; }
  :global(.stn-via) { font-size: 12px; margin-top: 2px; }
  :global(.via-rf) { color: var(--color-success); }
  :global(.via-rf-hops) { color: var(--color-warning); }
  :global(.via-is) { color: #c39bff; }
  :global(.stn-path) { color: var(--color-text-dim); font-size: 11px; }
  :global(.stn-path .path-link) { color: #6eb5ff; text-decoration: none; cursor: pointer; }
  :global(.stn-path .path-link:hover) { text-decoration: underline; }
  :global(.stn-comment) { color: var(--color-text-dim); font-style: italic; font-size: 12px; }
  :global(.stn-actions) { font-size: 12px; display: flex; gap: 12px; flex-wrap: wrap; }
  :global(.stn-link) { color: #6eb5ff; text-decoration: none; cursor: pointer; }
  :global(.stn-link:hover) { text-decoration: underline; }
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
