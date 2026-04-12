// Shared reactive map state using Svelte 5 runes.
//
// Uses .svelte.js extension so $state runes work. Exported as an object
// with getter/setter pairs so reactivity crosses module boundaries.
// localStorage sync for user preferences that persist across sessions.
// Initial center: localStorage → browser geolocation → US geographic center.

const US_CENTER = [39.8283, -98.5795];
const GEOLOCATED_ZOOM = 10;

function loadFloat(key, fallback) {
  const v = localStorage.getItem(key);
  return v != null ? parseFloat(v) : fallback;
}

function loadInt(key, fallback) {
  const v = localStorage.getItem(key);
  return v != null ? parseInt(v, 10) : fallback;
}

const hasSavedCenter = localStorage.getItem('map-center-lat') != null;

export const mapState = (() => {
  let selectedStation = $state(null);

  let layerToggles = $state({
    stations: true,
    aprsIs: true,
    trails: false,
    weather: false,
  });

  let timerange = $state(loadInt('map-timerange', 3600));
  let mapCenter = $state([
    loadFloat('map-center-lat', US_CENTER[0]),
    loadFloat('map-center-lon', US_CENTER[1]),
  ]);
  let mapZoom = $state(loadInt('map-zoom', 4));

  // If no saved center, try browser geolocation and update reactively
  if (!hasSavedCenter && 'geolocation' in navigator) {
    navigator.geolocation.getCurrentPosition(
      (pos) => {
        mapCenter = [pos.coords.latitude, pos.coords.longitude];
        mapZoom = GEOLOCATED_ZOOM;
      },
      () => {}, // denied or unavailable — keep US center
      { timeout: 5000, maximumAge: 300000 },
    );
  }

  return {
    get selectedStation() { return selectedStation; },
    set selectedStation(v) { selectedStation = v; },

    get layerToggles() { return layerToggles; },
    set layerToggles(v) { layerToggles = v; },

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
  };
})();
