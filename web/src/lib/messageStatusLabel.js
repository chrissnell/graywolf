// Human-readable status + attempt-count projection for a message row.
// Shared by MessageContextMenu's status footer (and available to any
// other surface that wants the same short wording). Deliberately
// separate from MessageBubble.svelte's inline `statusInfo` derivation,
// which drives icons/tooltips/click-to-resend affordances — a richer,
// different job than this compact single-line summary.
const STATUS_LABELS = {
  received: 'Received',
  queued: 'Waiting',
  tx_submitted: 'Waiting',
  sent_rf: 'Sent',
  sent_is: 'Sent',
  acked: 'Delivered',
  rejected: 'Rejected',
  timeout: 'Timed Out',
  failed: 'Failed',
  aborted: 'Aborted',
  sent: 'Broadcast', // tactical broadcast terminal state (dto.MessageStatusBroadcast)
};

/**
 * @param {{direction?: string, status?: string, attempts?: number}} msg
 * @param {boolean} isTactical
 * @param {number} retryMaxAttempts effective per-station retry cap
 * @returns {{label: string, attemptsText: string|null}}
 */
export function describeMessageStatus(msg, isTactical, retryMaxAttempts) {
  const status = msg?.status || '';
  const label = STATUS_LABELS[status] || (status || 'Unknown');
  const isOut = msg?.direction === 'out';
  const attempts = msg?.attempts || 0;
  // Attempts are hidden once delivered (nothing left to count), for
  // tactical rows (single-shot — no retry ladder to report on), and
  // before anything has actually been transmitted.
  const hideAttempts = !isOut || isTactical || status === 'acked' || attempts === 0;
  return {
    label,
    attemptsText: hideAttempts ? null : `${attempts}/${retryMaxAttempts}`,
  };
}
