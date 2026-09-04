// Reactive bulletins store — Svelte 5 runes + SvelteMap, mirroring
// messagesStore.svelte.js's singleton pattern.
//
// This is the single shared source of truth for inbound bulletins and
// their unread count. Previously Sidebar.svelte polled its own
// independent unread-only count every 30s, completely decoupled from
// Bulletins.svelte's own local list — so marking a bulletin read on the
// page could take up to 30s to clear the sidebar dot. Now both read from
// this one store, populated by the single shared poll in
// bulletinsTransport.js, so a mark-read is instantly visible everywhere.
//
// Module-level singleton so Sidebar.svelte and Bulletins.svelte observe
// the same state without prop plumbing.

import { SvelteMap } from 'svelte/reactivity';
import { diffNewlyUnread } from './bulletins-diff-core.js';

class BulletinsStore {
  // id -> BulletinResponse
  inbound = new SvelteMap();

  // True once the first GET /bulletins?direction=in snapshot has landed.
  loaded = $state(false);

  // True while Bulletins.svelte is mounted — suppresses new-bulletin
  // popups for a page the operator is already looking at (parity with
  // messagesStore's activeThreadId suppression).
  pageActive = $state(false);

  get unreadTotal() {
    let n = 0;
    for (const b of this.inbound.values()) {
      if (b.unread) n++;
    }
    return n;
  }

  get inboundList() {
    return [...this.inbound.values()].sort((a, b) =>
      (b.updated_at || '').localeCompare(a.updated_at || ''),
    );
  }

  /**
   * Replace the full inbound snapshot from the server. Returns the rows
   * that are newly unread since the previous snapshot (see
   * bulletins-diff-core.js), for the caller to raise notifications for.
   * @param {Array<object>} rows dto.BulletinResponse[]
   * @returns {Array<object>}
   */
  replaceInbound(rows) {
    const newly = diffNewlyUnread(this.inbound, rows || []);
    const seen = new Set();
    for (const b of rows || []) {
      if (!b || typeof b.id !== 'number') continue;
      seen.add(b.id);
      this.inbound.set(b.id, b);
    }
    for (const id of this.inbound.keys()) {
      if (!seen.has(id)) this.inbound.delete(id);
    }
    this.loaded = true;
    return newly;
  }

  markRead(id) {
    const b = this.inbound.get(id);
    if (b) this.inbound.set(id, { ...b, unread: false });
  }

  /** Rollback for a failed markRead POST. */
  markUnreadLocal(id) {
    const b = this.inbound.get(id);
    if (b) this.inbound.set(id, { ...b, unread: true });
  }

  markAllRead() {
    for (const [id, b] of this.inbound) {
      if (b.unread) this.inbound.set(id, { ...b, unread: false });
    }
  }

  removeLocal(id) {
    this.inbound.delete(id);
  }

  setPageActive(v) {
    this.pageActive = !!v;
  }
}

export const bulletinsStore = new BulletinsStore();
