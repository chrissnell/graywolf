// Pure diffing helper for bulletinsStore.svelte.js's replaceInbound(): on
// each poll, decides which rows in a fresh snapshot are "newly unread"
// compared to the previous snapshot, so bulletinsTransport.js knows which
// ones should raise a notification.
//
// A row is "newly unread" if it's brand new, or its unread flag flipped
// false->true, or it was re-heard (updated_at moved) — per
// docs/wiki/bulletins.md, a re-heard inbound bulletin upserts the
// existing row in place rather than accumulating duplicates, and that
// re-heard row is worth a fresh look even if it never went fully read.

/**
 * @param {Map<number, object>} prevById previous snapshot, keyed by id
 * @param {Array<object>} freshRows latest GET /bulletins?direction=in rows
 * @returns {Array<object>} the subset of freshRows that are newly unread
 */
export function diffNewlyUnread(prevById, freshRows) {
  const newly = [];
  for (const row of freshRows || []) {
    if (!row || !row.unread) continue;
    const prev = prevById.get(row.id);
    if (!prev || !prev.unread || prev.updated_at !== row.updated_at) {
      newly.push(row);
    }
  }
  return newly;
}
