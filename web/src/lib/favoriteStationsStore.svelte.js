// Reactive favorite-stations store — Svelte 5 runes, mirroring
// bulletinsStore.svelte.js's module-level singleton pattern.
//
// Single shared source of truth for the operator's favorite-station
// list (server-side, shared across every device pointed at this
// graywolf instance — see configstore.FavoriteStation). Three
// consumers read from this one instance: the map station popup (star
// toggle + fill state), NotificationsSettings.svelte (list/add/remove
// UI), and stationNewTransport.js (has() check on every poll, so a
// favorite added from one tab/device is picked up by the next poll
// without a reload).
//
// has()/find() match with the same wildcard semantics as
// pkg/messages/blocklist_set.go's BlocklistSet.Blocked() (see
// callsign-match-core.js): a favorite stored bare (e.g. "KV4S") matches
// every SSID of that base call, so the operator doesn't have to add
// "KV4S-1", "KV4S-9", etc. individually -- but it does NOT match an
// unrelated callsign that merely shares a prefix (e.g. "KV4SM").

import {
  listFavoriteStations,
  createFavoriteStation,
  deleteFavoriteStation,
} from '../api/stations.js';
import { callsignMatches } from './callsign-match-core.js';

class FavoriteStationsStore {
  // FavoriteStationResponse[]
  entries = $state([]);

  // True once the first GET /stations/favorites has landed.
  loaded = $state(false);

  async load() {
    try {
      const rows = await listFavoriteStations();
      this.entries = Array.isArray(rows) ? rows : [];
      this.loaded = true;
    } catch {
      // Leave stale state; caller (or the next transport poll) can retry.
    }
  }

  /** @param {string} callsign a heard callsign, exact SSID as heard on the air */
  has(callsign) {
    if (!callsign) return false;
    return this.entries.some((e) => callsignMatches(e.callsign, callsign));
  }

  /** @param {string} callsign the entry (bare or SSID-qualified) whose match rule covers this heard callsign, or null. */
  find(callsign) {
    if (!callsign) return null;
    return this.entries.find((e) => callsignMatches(e.callsign, callsign)) || null;
  }

  /**
   * @param {string} callsign
   * @param {string} [note]
   */
  async add(callsign, note = '') {
    await createFavoriteStation(callsign, note);
    await this.load();
  }

  /** @param {number} id */
  async remove(id) {
    await deleteFavoriteStation(id);
    await this.load();
  }

  /** Convenience for the map popup star toggle: add or remove by callsign. */
  async toggle(callsign) {
    const existing = this.find(callsign);
    if (existing) {
      await this.remove(existing.id);
    } else {
      await this.add(callsign);
    }
  }
}

export const favoriteStationsStore = new FavoriteStationsStore();
