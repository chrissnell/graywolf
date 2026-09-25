// Station emergency alerts transport — polls GET /api/stations/alerts
// (server-filtered to stations currently in Mic-E Emergency status,
// APRS101 ch 10 table 8) and raises a popup/OS/sound notification the
// first time a callsign appears, mirroring bulletinsTransport.js's
// shared-poll shape.
//
// Alerts (not just a passive map badge) are Emergency-only by design:
// real APRS hardware (Kenwood/Yaesu/Icom) only sounds an alarm for
// Emergency, not Priority/Special/etc, so this transport matches that
// behavior rather than interrupting the operator for every tactical
// status change. See docs/wiki/notifications.md.
//
// Lifecycle: start() is called once at App startup (see App.svelte),
// alongside messagesTransport's and bulletinsTransport's start().
// Polling is always-on for the session so an Emergency broadcast is
// caught regardless of which page or map viewport is active.

import { listStationAlerts } from '../api/stations.js';
import { diffNewlyAlerting } from './station-alerts-diff-core.js';
import { notifications } from './notificationsStore.svelte.js';
import { fireOsNotification } from './osNotify.js';
import { notificationPrefsState } from './settings/notification-prefs-store.svelte.js';
import { notificationSoundState } from './settings/notification-sound-store.svelte.js';
import { notificationsLogStore } from './notificationsLogStore.svelte.js';

const POLL_MS = 30_000;

let timer = null;
let started = false;
let stopped = false;

// Callsigns notified in a previous poll. A notification fires once per
// transition into Emergency, not on every poll while it persists; a
// callsign that clears and later re-asserts Emergency notifies again.
let activeCallsigns = new Set();

async function poll() {
  if (stopped) return;
  try {
    const rows = (await listStationAlerts()) || [];
    const newly = diffNewlyAlerting(activeCallsigns, rows);
    const nextActive = new Set(rows.map((r) => r.callsign));

    if (notificationPrefsState.stationEmergencyEnabled) {
      for (const r of newly) {
        // lat/lon are required, not just callsign -- LiveMapV2.svelte's
        // parseFocusFromHash() discards the whole deep-link (including
        // the callsign) if either is missing/non-finite, since it also
        // drives the initial camera fly-to before the station is loaded
        // from a poll. StationAlertDTO always carries a position (the
        // alerts endpoint excludes positionless stations -- see
        // pkg/webapi/stations.go's listStationAlerts).
        const href = `#/map?focus=${encodeURIComponent(r.callsign)}&lat=${r.lat}&lon=${r.lon}`;
        const body = r.status_text || 'Emergency';
        const title = `EMERGENCY: ${r.callsign}`;
        notificationsLogStore.add({ kind: 'station-emergency', title, body, href });
        if (notificationPrefsState.toastEnabled) {
          notifications.push({ kind: 'station-emergency', title, body, href });
        }
        fireOsNotification(title, body, () => {
          window.location.hash = href;
        });
        notificationSoundState.stationEmergency.play();
      }
    }

    activeCallsigns = nextActive;
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
