// Pure queue-management helpers for the new-activity popup stack.
// Split out from notificationsStore.svelte.js so the cap/dismiss logic
// is unit-testable without mounting Svelte (see notifications-core.test.js).

/**
 * Append entry, dropping the oldest if the stack exceeds maxStack.
 * @param {Array<object>} entries
 * @param {object} entry
 * @param {number} maxStack
 * @returns {Array<object>}
 */
export function addEntry(entries, entry, maxStack) {
  const next = [...entries, entry];
  return next.length > maxStack ? next.slice(next.length - maxStack) : next;
}

/**
 * Remove the entry with the given id, if present.
 * @param {Array<object>} entries
 * @param {string} id
 * @returns {Array<object>}
 */
export function removeEntry(entries, id) {
  return entries.filter((e) => e.id !== id);
}
