// Shared reactive map state using Svelte 5 runes.
//
// Uses .svelte.js extension so $state runes work. Exported as an object
// with getter/setter pairs so reactivity crosses module boundaries.
// localStorage sync for user preferences that persist across sessions.
// Initial center: localStorage → world view. The station's own position
// (when available from /api/position) is applied by LiveMapV2 as a
// one-shot recentering after the data store first reports it.

import { RADAR_REGION_US, RADAR_REGION_WORLD } from './sources/radar-source.js';

// Slightly above the equator so the world view shows more land than ocean.
const WORLD_CENTER = [20, 0];
const WORLD_ZOOM = 2;
// Zoom used when LiveMapV2 recenters on the station's "My Position".
export const MY_POSITION_ZOOM = 10;

function loadFloat(key, fallback) {
  const v = localStorage.getItem(key);
  return v != null ? parseFloat(v) : fallback;
}

function loadInt(key, fallback) {
  const v = localStorage.getItem(key);
  return v != null ? parseInt(v, 10) : fallback;
}

// Radar coverage region -- a per-browser map preference chosen on the maps
// settings tab and consumed by the radar layer on the live map. Only two
// values are valid; anything else falls back to US.
function normalizeRadarRegion(v) {
  return v === RADAR_REGION_WORLD ? RADAR_REGION_WORLD : RADAR_REGION_US;
}

const hasSavedCenter = localStorage.getItem('map-center-lat') != null;
const hasSavedZoom = localStorage.getItem('map-zoom') != null;
// Captured once at module load: did this browser already have a persisted
// map view when the page loaded? Consumers (LiveMapV2) use this to decide
// whether to apply the one-shot auto-fit-to-stations on first poll.
const hasSavedView = hasSavedCenter && hasSavedZoom;

export const mapState = (() => {
  let selectedStation = $state(null);

  let layerToggles = $state({
    stations: true,
    aprsIs: true,
    trails: true,
    weather: false,
    myPosition: false,
  });

  let highContrastLabels = $state(localStorage.getItem('map-high-contrast-labels') === '1');

  let timerange = $state(loadInt('map-timerange', 3600));
  let mapCenter = $state([
    loadFloat('map-center-lat', WORLD_CENTER[0]),
    loadFloat('map-center-lon', WORLD_CENTER[1]),
  ]);
  let mapZoom = $state(loadInt('map-zoom', WORLD_ZOOM));
  let radarRegion = $state(normalizeRadarRegion(localStorage.getItem('gw_radar_region')));

  return {
    get selectedStation() { return selectedStation; },
    set selectedStation(v) { selectedStation = v; },

    get layerToggles() { return layerToggles; },
    set layerToggles(v) { layerToggles = v; },

    get highContrastLabels() { return highContrastLabels; },
    set highContrastLabels(v) {
      highContrastLabels = v;
      localStorage.setItem('map-high-contrast-labels', v ? '1' : '0');
    },

    get timerange() { return timerange; },
    set timerange(v) {
      timerange = v;
      localStorage.setItem('map-timerange', String(v));
    },

    get mapCenter() { return mapCenter; },
    set mapCenter(v) {
      mapCenter = v;
      localStorage.setItem('map-center-lat', String(v[0]));
      localStorage.setItem('map-center-lon', String(v[1]));
    },

    get mapZoom() { return mapZoom; },
    set mapZoom(v) {
      mapZoom = v;
      localStorage.setItem('map-zoom', String(v));
    },

    get radarRegion() { return radarRegion; },
    set radarRegion(v) {
      const next = normalizeRadarRegion(v);
      radarRegion = next;
      localStorage.setItem('gw_radar_region', next);
    },

    hasSavedView,
  };
})();
