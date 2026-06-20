// Tracks whether this browser currently has contact with the Graywolf
// server. The REST client (lib/api.js) and the live-map data store both
// report into this store: a genuine network failure (fetch throws) marks
// the connection lost; any response from the server -- even an HTTP error --
// proves the server is reachable and marks it restored.
//
// Screens read `online` to swap stale/fabricated data for placeholder
// dashes and a prominent lost-connection indicator instead of silently
// rendering the last-known values as if they were live (GH #365).
//
// Plain svelte/store (not a runes module) on purpose: lib/api.js imports
// this, and lib/api.js is loaded by node --test, which has no Svelte
// compiler to resolve $state. A writable works in both contexts.

import { writable } from 'svelte/store';

// Optimistic: assume connected until a fetch actually fails, so first paint
// doesn't flash a disconnect indicator before any request has run.
export const online = writable(true);

export function markConnected() {
  online.set(true);
}

export function markDisconnected() {
  online.set(false);
}
