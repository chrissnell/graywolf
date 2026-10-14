// Fires an OS-level browser Notification for new activity, gated by the
// operator's notification-prefs-store mode and (unless forced) whether
// the tab is currently hidden/unfocused — avoids double-signaling (an
// in-app popup AND an OS banner) while the operator is already looking
// at graywolf. `force` is used by the Preferences "Send test
// notification" button, where the tab is necessarily focused.

import { notificationPrefsState } from './settings/notification-prefs-store.svelte.js';
import { shouldFireOsNotification } from './notification-rules-core.js';

/**
 * @param {string} title
 * @param {string} body
 * @param {(() => void)|null} [onClick] called (and the window refocused)
 *   when the operator clicks the OS notification.
 * @param {{force?: boolean}} [opts]
 */
export function fireOsNotification(title, body, onClick, { force = false } = {}) {
  if (typeof window === 'undefined' || typeof Notification === 'undefined') {
    console.warn('[osNotify] window.Notification is unavailable in this environment');
    return;
  }
  const documentHidden = typeof document !== 'undefined' && (document.hidden || !document.hasFocus());
  const gate = {
    enabled: notificationPrefsState.osEnabled,
    permission: Notification.permission,
    documentHidden,
    force,
  };
  if (!shouldFireOsNotification(gate)) {
    // Visibility into *why* it didn't fire — previously a silent no-op,
    // which made "granted but nothing appeared" indistinguishable from
    // "gated by mode/focus" from the console. See graywolf notification
    // troubleshooting notes in docs/wiki/notifications.md.
    console.warn('[osNotify] suppressed:', gate);
    return;
  }
  try {
    const n = new Notification(title, { body });
    if (onClick) {
      n.onclick = () => {
        window.focus?.();
        onClick();
        n.close();
      };
    }
  } catch (err) {
    // Platform quirk (e.g. a browser that lies about support). Logged
    // rather than silently swallowed — a granted-but-non-displaying
    // notification (OS-level blocking, Focus Assist, etc.) looks
    // identical to a thrown construction error from the caller's POV,
    // so this is the only signal that distinguishes them.
    console.warn('[osNotify] Notification construction failed:', err);
  }
}
