// Bulletins transport — single shared poll, replacing the two
// independent 30s polls that used to exist (Sidebar.svelte's unread-only
// count and Bulletins.svelte's own inbound fetch). Populates
// bulletinsStore.svelte.js and, on newly-unread rows, raises a
// new-activity notification (in-app popup and/or OS notification,
// per the operator's notification-prefs-store mode) plus a notification
// sound per notification-sound-store's bulletin settings.
//
// Lifecycle: start() is called once at App startup (see App.svelte),
// alongside messagesTransport's start(). Polling is always-on for the
// session so the sidebar bulletin badge stays fresh from any page.

import { bulletinsStore } from './bulletinsStore.svelte.js';
import { listBulletins } from '../api/bulletins.js';
import { notifications } from './notificationsStore.svelte.js';
import { shouldNotifyBulletin } from './notification-rules-core.js';
import { fireOsNotification } from './osNotify.js';
import { notificationPrefsState } from './settings/notification-prefs-store.svelte.js';
import { notificationSoundState } from './settings/notification-sound-store.svelte.js';
import { notificationsLogStore } from './notificationsLogStore.svelte.js';

const POLL_MS = 30_000;

let timer = null;
let started = false;
let stopped = false;

async function poll() {
  if (stopped) return;
  try {
    const rows = await listBulletins({ direction: 'in' });
    const newly = bulletinsStore.replaceInbound(rows || []);
    for (const b of newly) {
      if (!notificationPrefsState.bulletinEnabled) continue;
      if (!shouldNotifyBulletin({ pageActive: bulletinsStore.pageActive })) continue;
      const href = `#/bulletins?focus=${b.id}`;
      const title = `Bulletin from ${b.from_call} (${b.slot})`;
      notificationsLogStore.add({ kind: 'bulletin', title, body: b.text, href });
      if (notificationPrefsState.toastEnabled) {
        notifications.push({ kind: 'bulletin', title, body: b.text, href });
      }
      fireOsNotification(`Bulletin from ${b.from_call}`, b.text, () => {
        window.location.hash = href;
      });
      notificationSoundState.bulletin.play();
    }
  } catch {
    // Leave state as-is; the next poll retries.
  }
}

/** Start the shared poll. Safe to call multiple times; a no-op after the first. */
export function start() {
  if (started || stopped) return;
  started = true;
  poll();
  timer = setInterval(poll, POLL_MS);
}

/** Stop the poll. Exposed for Vite HMR / tests — production never calls this. */
export function stop() {
  stopped = true;
  started = false;
  clearInterval(timer);
  timer = null;
}

// Vite HMR: without this, editing this file (or anything it imports)
// while `npm run dev` is running leaves the OLD module instance's
// setInterval running forever alongside the new one, stacking duplicate
// poll loops that each independently fire their own notifications for
// the same event (2026-08-01, see stationNewTransport.js's identical fix
// for the report that surfaced this). No-op in a production build.
if (import.meta.hot) {
  import.meta.hot.dispose(() => stop());
}
