// Reactive release-notes store. Mirrors the IIFE + getter/setter
// pattern of lib/settings/units-store.svelte.js — state lives in
// $state, exposed via getters so components read `releaseNotes.unseen`
// directly and get fine-grained reactivity.
//
// The server is the source of truth (ack is an API POST, not
// localStorage). This store is memory-only; it re-fetches on every
// page load.
//
// The pure sort/filter/fetch/ack logic lives in releaseNotesCore.js so
// it can be unit-tested under `node --test` without a Svelte compile
// step (mirrors the channelsCore split).

import { toasts } from './stores.js';
import { makeReleaseNotesCore } from './releaseNotesCore.js';

export const releaseNotes = (() => {
  // $state rune — components that read `releaseNotes.unseen` etc. get
  // fine-grained reactivity because the getters below return the runes
  // directly (not snapshots).
  let unseen = $state([]);
  let all = $state([]);
  let loading = $state(false);
  let error = $state(null);
  let current = $state('');
  let lastSeen = $state('');

  // The core mutates this object's fields. In Svelte 5, assigning to
  // `unseen` via `state.unseen = ...` updates the underlying $state.
  const state = {
    get unseen() { return unseen; },
    set unseen(v) { unseen = v; },
    get all() { return all; },
    set all(v) { all = v; },
    get loading() { return loading; },
    set loading(v) { loading = v; },
    get error() { return error; },
    set error(v) { error = v; },
    get current() { return current; },
    set current(v) { current = v; },
    get lastSeen() { return lastSeen; },
    set lastSeen(v) { lastSeen = v; },
  };

  const core = makeReleaseNotesCore({ state, toasts });

  return {
    get unseen() { return unseen; },
    get all() { return all; },
    get loading() { return loading; },
    get error() { return error; },
    get current() { return current; },
    get lastSeen() { return lastSeen; },
    fetchUnseen: core.fetchUnseen,
    fetchAll: core.fetchAll,
    ack: core.ack,
  };
})();
