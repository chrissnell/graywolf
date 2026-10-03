// Reactive queue store backing the clickable new-activity popup stack
// (NotificationPopup.svelte). Wraps the pure push/cap/dismiss logic in
// notifications-core.js with Svelte 5 runes.
//
// Module-level singleton so both trigger sources (messagesTransport.js
// for new DMs/tacticals, bulletinsTransport.js for new bulletins, and
// Preferences.svelte's "Send test notification" button) share one stack
// with NotificationPopup.svelte.

import { addEntry, removeEntry } from './notifications-core.js';

const MAX_STACK = 4;
const AUTO_DISMISS_MS = 8000;

class NotificationsStore {
  entries = $state([]);

  /**
   * Push a new notification. Auto-dismisses after `autoDismissMs` unless
   * 0 is passed. Returns the generated id.
   * @param {{kind: string, title: string, body?: string, href?: string, autoDismissMs?: number}} entry
   */
  push({ kind, title, body = '', href = '', autoDismissMs = AUTO_DISMISS_MS }) {
    const id = `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
    this.entries = addEntry(
      this.entries,
      { id, kind, title, body, href, createdAt: Date.now() },
      MAX_STACK,
    );
    if (autoDismissMs > 0) {
      setTimeout(() => this.dismiss(id), autoDismissMs);
    }
    return id;
  }

  dismiss(id) {
    this.entries = removeEntry(this.entries, id);
  }
}

export const notifications = new NotificationsStore();
