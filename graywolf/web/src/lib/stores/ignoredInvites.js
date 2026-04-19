// Client-only "ignored invite" tracking.
//
// Ignoring an invite is local cosmetic state — there's no server column
// and no endpoint (per the tactical-chat-invite plan §9). The operator
// clicks away from an invite bubble and it collapses; on refresh the
// same set persists from localStorage. Cross-device sync is deliberately
// out of scope.
//
// Shape: `Set<number>` of message IDs. Exposed as a Svelte writable
// store so components can subscribe reactively, plus imperative helpers
// for the common read/write paths. The project's other messages stores
// (messagesStore.svelte.js) use Svelte 5 runes + SvelteMap, but those
// patterns don't compose cleanly with localStorage hydration at import
// time; a plain `writable` with a SvelteMap-free `Set` is the simpler
// model here.

import { writable } from 'svelte/store';

const STORAGE_KEY = 'graywolf.ignoredInviteIds';

function loadFromStorage() {
  if (typeof localStorage === 'undefined') return new Set();
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) return new Set();
    const parsed = JSON.parse(raw);
    if (!Array.isArray(parsed)) return new Set();
    const out = new Set();
    for (const v of parsed) {
      const n = Number(v);
      if (Number.isFinite(n) && n > 0) out.add(n);
    }
    return out;
  } catch {
    return new Set();
  }
}

function saveToStorage(set) {
  if (typeof localStorage === 'undefined') return;
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify([...set]));
  } catch {
    // Quota exceeded, privacy mode, etc — non-fatal.
  }
}

/**
 * Svelte store holding the current ignored-invite set. Subscribe to
 * rebuild UI when invites are ignored/unignored from anywhere in the app.
 *
 * @type {import('svelte/store').Writable<Set<number>>}
 */
export const ignoredInviteIds = writable(loadFromStorage());

// Persist on every mutation. Subscription fires synchronously on
// creation too, but writing the same thing back out once is cheap.
ignoredInviteIds.subscribe((v) => {
  if (v instanceof Set) saveToStorage(v);
});

/** Mark a message ID as ignored (idempotent). */
export function ignoreInvite(id) {
  const n = Number(id);
  if (!Number.isFinite(n) || n <= 0) return;
  ignoredInviteIds.update((s) => {
    if (s.has(n)) return s;
    const next = new Set(s);
    next.add(n);
    return next;
  });
}

/** Remove the ignore flag (idempotent). */
export function unignoreInvite(id) {
  const n = Number(id);
  if (!Number.isFinite(n) || n <= 0) return;
  ignoredInviteIds.update((s) => {
    if (!s.has(n)) return s;
    const next = new Set(s);
    next.delete(n);
    return next;
  });
}

/**
 * Synchronous peek. Intended for non-reactive callers (tests, imperative
 * guards). Reactive UI should subscribe via `$ignoredInviteIds`.
 */
export function isIgnored(id) {
  const n = Number(id);
  if (!Number.isFinite(n) || n <= 0) return false;
  let current;
  const unsub = ignoredInviteIds.subscribe((v) => { current = v; });
  unsub();
  return !!current?.has(n);
}

// --- Auto-nav-once tracking --------------------------------------
// The plan says auto-nav happens on the FIRST accept of an invite
// bubble in a session, but subsequent renders (refresh, other tabs,
// multiple invites to the same TAC) must NOT auto-nav. This set lives
// in module scope (ES module singleton, not persisted) so it resets on
// page reload — "first accept this session" is a per-session property.
const autoNavDone = new Set();

export function markAutoNavDone(id) {
  const n = Number(id);
  if (Number.isFinite(n) && n > 0) autoNavDone.add(n);
}

export function hasAutoNavFired(id) {
  const n = Number(id);
  if (!Number.isFinite(n) || n <= 0) return false;
  return autoNavDone.has(n);
}
