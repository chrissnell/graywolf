// Pure helpers for the chat window's optimistic unread-count updates.
// Kept free of Svelte runes so they run under `node --test`.

/**
 * Group a read-marking batch by thread.
 *
 * @param {Array<[number, {kind: string, key: string}]>} entries
 *   [messageId, {kind, key}] pairs, where kind/key come from the message
 *   row itself (thread_kind/thread_key).
 * @param {(kind: string, key: string) => string} threadIdFor
 * @returns {Map<string, number>} threadId -> number of messages in the batch
 */
export function countByThread(entries, threadIdFor) {
  const out = new Map();
  for (const [, t] of entries) {
    if (!t) continue;
    const tid = threadIdFor(t.kind, t.key);
    out.set(tid, (out.get(tid) || 0) + 1);
  }
  return out;
}

/**
 * Apply a delta to an unread count, never going below zero.
 *
 * @param {number | undefined} current
 * @param {number} delta  negative to decrement, positive to roll back
 * @returns {number}
 */
export function adjustUnread(current, delta) {
  return Math.max(0, (current || 0) + delta);
}
