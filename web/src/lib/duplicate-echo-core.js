// Collapses consecutive inbound bubbles that are the same physical
// packet heard on multiple paths (RF direct + APRS-IS gate, or a
// digipeater re-tx) into a single display bubble carrying every path's
// source badge.
//
// Backend context: messages.Router only dedups on (from, msgid,
// text_hash) and only when the packet carries a MessageID (see
// pkg/messages/router.go — invariant #27). Well-known no-ack bots
// (WXBOT and friends, see pkg/messages/bots.go) send unnumbered
// messages, so multi-path echoes of the exact same text persist as
// separate rows. This is a display-only fix: the underlying rows and
// their unread state are untouched, so callers MUST still mark every
// id in `mergedIds` read together (see MessageThread.svelte's dwell
// batching) — the server still has one row per path.
//
// Only merges adjacent entries in the (already chronologically sorted)
// input array, so an interleaved message from someone else — or from
// us — breaks the run rather than being merged across.

const DEFAULT_WINDOW_MS = 10_000;

function timestampOf(m) {
  return Date.parse(m?.sent_at || m?.received_at || m?.created_at || 0) || 0;
}

/**
 * @param {Array<any>} messages - chronologically ascending
 * @param {{ windowMs?: number }} [opts] - max gap between echoes to merge
 * @returns {Array<any>} display bubbles; each carries `mergedIds` (every
 *   underlying row id, primary first) and `mergedSources` (unique
 *   source values, first-seen order)
 */
export function collapseDuplicateEchoes(messages, opts = {}) {
  const windowMs = opts.windowMs ?? DEFAULT_WINDOW_MS;
  /** @type {Array<any>} */
  const out = [];
  let lastTs = 0;

  for (const m of messages || []) {
    const ts = timestampOf(m);
    const prev = out[out.length - 1];
    const canMerge =
      !!prev &&
      m?.direction === 'in' &&
      prev.direction === 'in' &&
      prev.kind !== 'invite' &&
      m?.kind !== 'invite' &&
      (m?.from_call || '') === (prev.from_call || '') &&
      (m?.text || '') === (prev.text || '') &&
      ts - lastTs <= windowMs;

    if (canMerge) {
      if (m?.source && !prev.mergedSources.includes(m.source)) {
        prev.mergedSources.push(m.source);
      }
      prev.mergedIds.push(m?.id);
      prev.unread = prev.unread || !!m?.unread;
    } else {
      out.push({
        ...m,
        mergedIds: [m?.id],
        mergedSources: m?.source ? [m.source] : [],
      });
    }
    lastTs = ts;
  }

  return out;
}
