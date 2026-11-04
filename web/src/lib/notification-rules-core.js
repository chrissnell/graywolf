// Pure suppression-rule helpers for the new-activity popup feature.
// Kept separate from the transport wiring (messagesTransport.js,
// bulletinsTransport.js) and the OS notification firer (osNotify.js) so
// the decision logic is unit-testable without mounting Svelte or a real
// Notification API (see notification-rules-core.test.js).

/**
 * Should a new inbound message raise a notification?
 * @param {{muted: boolean, isActiveThread: boolean}} ctx
 */
export function shouldNotifyMessage({ muted, isActiveThread }) {
  if (muted) return false;
  if (isActiveThread) return false;
  return true;
}

/**
 * Should a newly-unread bulletin raise a notification?
 * @param {{pageActive: boolean}} ctx
 */
export function shouldNotifyBulletin({ pageActive }) {
  return !pageActive;
}

/**
 * Should an OS-level Notification actually fire? `force` bypasses the
 * document-hidden requirement for the Preferences "Send test
 * notification" button, where the operator is by definition looking at
 * the tab.
 * @param {{enabled: boolean, permission: string, documentHidden: boolean, force?: boolean}} ctx
 */
export function shouldFireOsNotification({ enabled, permission, documentHidden, force = false }) {
  return !!enabled && permission === 'granted' && (!!force || !!documentHidden);
}
