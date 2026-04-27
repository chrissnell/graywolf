// Shared reactive store for the channel list + their live backing
// state (D9 in the KISS tcp-client plan). Every picker page and the
// Channels page subscribes to this store rather than fetching
// /api/channels independently — a single poller + invalidate() keeps
// the UI coherent so a flapping KISS-TNC doesn't look healthy on one
// tab and down on another.
//
// Svelte 5 runes: the exported `channelsStore` object is `$state` so
// consumers can read `channelsStore.list` directly in templates and
// `$derived` expressions and get fine-grained reactivity. Don't
// reassign the object itself — mutate fields in place.
//
// Shape of each list entry (mirrors dto.ChannelResponse):
//   { id, name, input_device_id, ..., backing: { modem, kiss_tnc,
//     summary, health } }
//
// Testability note: the polling + fetch core lives in plain JS in
// channelsCore.js and is exercised by channelsCore.test.js under
// `node --test`. This file wraps that core in a $state object +
// registers window-focus and timer hooks — the "reactive surface"
// that must be compiled through Svelte.

import { makeChannelsCore } from './channelsCore.js';

// Default poll interval. 5 s matches D9 in the plan and keeps live
// backing state fresh enough that a KISS flap surfaces within the
// typical operator glance cycle, without pounding a tiny API.
const DEFAULT_POLL_MS = 5_000;

// Consumers flag the store as "stale" after this many ms without a
// refresh so pickers can render a subtle indicator. Kept here (not a
// const in the consumer) so the rule stays in one place.
export const STALE_AFTER_MS = 30_000;

// Reactive state object. Consumers access fields directly
// (`channelsStore.list`, `channelsStore.loading`, etc.). Mutations
// by the poller land on these fields, not on the object itself.
export const channelsStore = $state({
  list: [],
  loading: true,
  error: null,
  lastUpdated: null,
});

// Shared core. The $state object above is handed to the core as the
// mutation sink so the core stays pure-JS-testable (no rune runtime
// needed in tests).
const _core = makeChannelsCore({
  state: channelsStore,
  defaultPollMs: DEFAULT_POLL_MS,
});

/**
 * Kick off polling. Idempotent — a second call is a no-op. The
 * caller owns the interval but start() picks the plan's D9 5 s
 * default.
 *
 * @param {{intervalMs?: number}} [opts]
 */
export function start(opts) {
  _core.start(opts);
}

/**
 * Stop polling and release the focus listener. Tests should always
 * call this in teardown; app code rarely needs it because the store
 * lives for the full SPA session.
 */
export function stop() {
  _core.stop();
}

/**
 * Trigger an immediate refetch. Coalesces: multiple overlapping
 * calls return the same in-flight promise. Returns a promise that
 * resolves when the refetch completes (success or error — check
 * `channelsStore.error`).
 */
export function invalidate() {
  return _core.invalidate();
}

/**
 * True when the last successful refresh was more than STALE_AFTER_MS
 * ago. Consumers render a muted "stale" indicator on the picker
 * trigger.
 */
export function isStale() {
  if (channelsStore.lastUpdated == null) return false;
  return Date.now() - channelsStore.lastUpdated > STALE_AFTER_MS;
}

/**
 * Look up a channel by id from the current list. Returns undefined
 * when the id is not present. Kept here so callers don't duplicate
 * the search.
 */
export function getChannel(id) {
  const n = typeof id === 'string' ? parseInt(id, 10) : id;
  return channelsStore.list.find((c) => c.id === n);
}
