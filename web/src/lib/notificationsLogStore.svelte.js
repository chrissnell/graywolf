// Reactive device-local notifications log — Svelte 5 runes, mirroring
// favoriteStationsStore.svelte.js's module-level singleton pattern.
//
// Every notification-firing site (messagesTransport.js, bulletinsTransport.js,
// stationAlertsTransport.js, stationNewTransport.js) calls add() right
// alongside its toast/OS/sound calls, so this ends up as a persisted
// history of everything this device has actually been notified about --
// shown on the Notifications Log page. See notifications-log-core.js's
// header comment for why this is device-local rather than server-side.

import { parseLogEntries, addLogEntry } from './notifications-log-core.js';

const LS_KEY = 'gw-notifications-log';

function safeGetItem(key) {
  try {
    return localStorage.getItem(key);
  } catch {
    return null;
  }
}

function safeSetItem(key, value) {
  try {
    localStorage.setItem(key, value);
  } catch {
    /* ignore */
  }
}

function makeId() {
  return `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
}

class NotificationsLogStore {
  // Newest-first.
  entries = $state(parseLogEntries(safeGetItem(LS_KEY)));

  /**
   * @param {{kind: string, title: string, body?: string, href?: string}} entry
   */
  add(entry) {
    const full = {
      id: makeId(),
      kind: entry.kind,
      title: entry.title,
      body: entry.body || '',
      href: entry.href || '',
      timestamp: Date.now(),
    };
    this.entries = addLogEntry(this.entries, full);
    safeSetItem(LS_KEY, JSON.stringify(this.entries));
  }

  clear() {
    this.entries = [];
    safeSetItem(LS_KEY, JSON.stringify(this.entries));
  }
}

export const notificationsLogStore = new NotificationsLogStore();
