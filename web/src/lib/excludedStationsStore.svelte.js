// Reactive excluded-stations store — Svelte 5 runes, mirroring
// favoriteStationsStore.svelte.js's module-level singleton pattern
// exactly (the inverse concept: server-side, shared across every device
// pointed at this graywolf instance — see configstore.ExcludedStation).
//
// stationNewTransport.js checks has() before the favorites check on
// every poll, so an exclusion wins even over a favorite. Same wildcard
// base-callsign matching as favorites via callsign-match-core.js's
// callsignMatches.

import {
  listStationExclusions,
  createStationExclusion,
  deleteStationExclusion,
} from '../api/stations.js';
import { callsignMatches } from './callsign-match-core.js';

class ExcludedStationsStore {
  // ExcludedStationResponse[]
  entries = $state([]);

  // True once the first GET /stations/exclusions has landed.
  loaded = $state(false);

  async load() {
    try {
      const rows = await listStationExclusions();
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
    await createStationExclusion(callsign, note);
    await this.load();
  }

  /** @param {number} id */
  async remove(id) {
    await deleteStationExclusion(id);
    await this.load();
  }

  /** Convenience for the map popup exclude toggle: add or remove by callsign. */
  async toggle(callsign) {
    const existing = this.find(callsign);
    if (existing) {
      await this.remove(existing.id);
    } else {
      await this.add(callsign);
    }
  }
}

export const excludedStationsStore = new ExcludedStationsStore();
