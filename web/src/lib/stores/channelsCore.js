// channelsCore.js — pure-JS polling + invalidate engine for the
// shared channels store. Lives outside the .svelte.js wrapper so tests
// can exercise it under `node --test` without needing to compile runes.
//
// The caller (channels.svelte.js in app code, the test harness in
// tests) supplies:
//   - `state`: a mutable object with fields { list, loading, error,
//     lastUpdated }. The core mutates these fields in place; in the
//     real app this object is a Svelte 5 $state. In tests it's a plain
//     POJO.
//   - `fetchFn`: optional override of the fetch function. Defaults to
//     `api.get('/channels')`. Tests inject a spy.
//   - `defaultPollMs`: optional default for start({intervalMs}) when
//     the caller doesn't specify one.
//
// The core intentionally owns window.focus listener registration so
// the .svelte.js wrapper stays thin; tests that don't exercise focus
// behavior pass `skipFocusListener: true`.

import { api } from '../api.js';

export function makeChannelsCore(opts) {
  const state = opts.state;
  const defaultPollMs = opts.defaultPollMs ?? 5_000;
  const fetchFn = opts.fetchFn ?? (() => api.get('/channels'));
  const skipFocusListener = opts.skipFocusListener === true;

  let timer = null;
  let inflight = null;
  let started = false;
  let pollIntervalMs = defaultPollMs;
  let focusListenerAttached = false;

  function invalidate() {
    if (inflight) return inflight;
    inflight = (async () => {
      state.loading = true;
      try {
        const list = await fetchFn();
        state.list = Array.isArray(list) ? list : [];
        state.error = null;
        state.lastUpdated = Date.now();
      } catch (err) {
        state.error = err?.message || String(err);
      } finally {
        state.loading = false;
        inflight = null;
      }
    })();
    return inflight;
  }

  function scheduleNext() {
    if (!started) return;
    timer = setTimeout(async () => {
      await invalidate();
      scheduleNext();
    }, pollIntervalMs);
  }

  function onFocus() {
    invalidate();
  }

  function start(options) {
    if (started) return;
    started = true;
    if (options && typeof options.intervalMs === 'number' && options.intervalMs > 0) {
      pollIntervalMs = options.intervalMs;
    }
    if (!skipFocusListener && !focusListenerAttached && typeof window !== 'undefined') {
      window.addEventListener('focus', onFocus);
      focusListenerAttached = true;
    }
    invalidate();
    scheduleNext();
  }

  function stop() {
    if (timer) {
      clearTimeout(timer);
      timer = null;
    }
    if (focusListenerAttached && typeof window !== 'undefined') {
      window.removeEventListener('focus', onFocus);
      focusListenerAttached = false;
    }
    started = false;
  }

  return { start, stop, invalidate };
}
