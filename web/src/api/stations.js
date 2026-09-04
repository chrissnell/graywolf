import { api } from '../lib/api.js';

// --- Stations -------------------------------------------------------------

/**
 * GET /api/stations/alerts — stations currently in Emergency status
 * (Mic-E message code 0, APRS101 ch 10 table 8), regardless of the
 * caller's map viewport. Polled by stationAlertsTransport.js to drive
 * the popup/OS/sound notification.
 * @returns {Promise<Array<{callsign: string, status_code: number, status_text: string, lat: number, lon: number, last_heard: string}>>}
 */
export function listStationAlerts() {
  return api.get('/stations/alerts');
}

/**
 * GET /api/stations/roster — every currently-heard station (compact,
 * objects/items excluded), regardless of the caller's map viewport.
 * Polled by stationNewTransport.js to detect stations that have gone
 * unheard long enough to count as new again.
 * @returns {Promise<Array<{callsign: string, symbol_table: string, symbol_code: string, lat: number, lon: number, direction: string, gated?: boolean, is_digipeater?: boolean, is_weather_station?: boolean, is_repeater?: boolean, last_direct_heard: string, last_heard: string}>>}
 */
export function listStationRoster() {
  return api.get('/stations/roster');
}

// --- Favorite stations ------------------------------------------------------

/**
 * GET /api/stations/favorites — every favorite station, server-side and
 * shared across every device pointed at this graywolf instance.
 * @returns {Promise<Array<{id: number, callsign: string, note?: string, created_at: string}>>}
 */
export function listFavoriteStations() {
  return api.get('/stations/favorites');
}

/**
 * POST /api/stations/favorites — add a callsign to the favorites list.
 * @param {string} callsign
 * @param {string} [note]
 */
export function createFavoriteStation(callsign, note = '') {
  return api.post('/stations/favorites', { callsign, note });
}

/**
 * DELETE /api/stations/favorites/{id} — remove a favorite.
 * @param {number} id
 */
export function deleteFavoriteStation(id) {
  return api.delete(`/stations/favorites/${id}`);
}

// --- Excluded stations -------------------------------------------------------

/**
 * GET /api/stations/exclusions — every excluded station, server-side and
 * shared across every device pointed at this graywolf instance.
 * @returns {Promise<Array<{id: number, callsign: string, note?: string, created_at: string}>>}
 */
export function listStationExclusions() {
  return api.get('/stations/exclusions');
}

/**
 * POST /api/stations/exclusions — add a callsign to the exclusion list.
 * @param {string} callsign
 * @param {string} [note]
 */
export function createStationExclusion(callsign, note = '') {
  return api.post('/stations/exclusions', { callsign, note });
}

/**
 * DELETE /api/stations/exclusions/{id} — remove an exclusion.
 * @param {number} id
 */
export function deleteStationExclusion(id) {
  return api.delete(`/stations/exclusions/${id}`);
}
