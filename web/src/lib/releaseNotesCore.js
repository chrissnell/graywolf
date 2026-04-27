// releaseNotesCore.js — pure-JS sort / filter / fetch / ack engine for
// the release-notes store. Lives outside the .svelte.js wrapper so tests
// can exercise it under `node --test` without needing to compile runes.
//
// The caller (releaseNotesStore.svelte.js in app code, the test harness
// in tests) supplies:
//   - `state`: a mutable object with fields
//     { unseen, all, loading, error, current }. The core mutates these
//     fields in place; in the real app this object is a Svelte 5 $state.
//     In tests it's a plain POJO.
//   - `fetchFn`: async (path) => parsed JSON. Defaults to `fetch(...).json()`
//     with `credentials: 'same-origin'`. Tests inject a spy.
//   - `postFn`: async (path) => Response-like {ok, status}. Defaults to
//     a same-origin POST. Tests inject a spy for ack.
//   - `toasts`: `{success(msg), error(msg)}` — chonky-ui's toaster.
//     Tests inject a recorder to assert which path fired.
//   - `waitFn`: (ms) => Promise. Defaults to setTimeout. Tests inject a
//     synchronous mock so the 2s backoff doesn't slow the suite.
//
// Sort and filter semantics mirror pkg/releasenotes/semver.Compare —
// the server already applies both, but we re-apply defensively so a bad
// response or a stale cached bundle can't put info cards above CTAs.

// Highest per-note schema_version the frontend knows how to render.
// Notes with schema_version > MAX_SCHEMA are silently dropped so a
// stale SPA bundle against a newer backend degrades gracefully (plan
// D9). Bump together with any breaking change to the rendered body
// format.
export const MAX_SCHEMA = 1;

// Compare "x.y.z" version strings returning -1/0/1. Treats empty
// string as less than any real version (matches pkg/releasenotes/
// semver.Compare's empty-string contract, so filter+sort stay
// consistent across server and client). Suffixes after the first
// non-digit-or-dot are stripped, so "0.11.0-beta.3" collapses to
// "0.11.0".
export function cmpVersion(a, b) {
  const aEmpty = !a;
  const bEmpty = !b;
  if (aEmpty && bEmpty) return 0;
  if (aEmpty) return -1;
  if (bEmpty) return 1;
  const norm = (s) => s.split(/[^0-9.]/)[0] || '';
  const pa = norm(a).split('.').map((n) => parseInt(n, 10) || 0);
  const pb = norm(b).split('.').map((n) => parseInt(n, 10) || 0);
  const len = Math.max(pa.length, pb.length, 3);
  for (let i = 0; i < len; i++) {
    const ai = pa[i] || 0;
    const bi = pb[i] || 0;
    if (ai < bi) return -1;
    if (ai > bi) return 1;
  }
  return 0;
}

// CTA-first, then version-desc. Belt-and-suspenders — the server
// already applies this order, but a stale cached response or a bad
// backend must not let info cards appear above CTAs.
export function sortNotes(notes) {
  return [...(notes || [])].sort((a, b) => {
    const aCta = a?.style === 'cta' ? 0 : 1;
    const bCta = b?.style === 'cta' ? 0 : 1;
    if (aCta !== bCta) return aCta - bCta;
    return cmpVersion(b?.version, a?.version);
  });
}

// Drop notes whose per-note schema_version exceeds MAX_SCHEMA. A note
// missing the field is assumed schema 1 (matches server default).
export function filterBySchema(notes, max = MAX_SCHEMA) {
  return (notes || []).filter((n) => {
    const sv = typeof n?.schema_version === 'number' ? n.schema_version : 1;
    return sv <= max;
  });
}

// Default fetch used in production. `fetchImpl` defaults to the global
// fetch but can be overridden for tests.
async function defaultFetchJSON(path, fetchImpl = globalThis.fetch) {
  const res = await fetchImpl(path, { credentials: 'same-origin' });
  if (res.status === 401) {
    throw new Error('unauthorized');
  }
  if (!res.ok) {
    throw new Error(`HTTP ${res.status}`);
  }
  return res.json();
}

function defaultWait(ms) {
  return new Promise((r) => setTimeout(r, ms));
}

// makeReleaseNotesCore returns { fetchUnseen, fetchAll, ack } operating
// on the supplied `state`. All three functions mutate `state` in place
// and return void (or the ack promise, for tests that want to await).
export function makeReleaseNotesCore(opts) {
  const state = opts.state;
  const fetchJSON = opts.fetchFn ?? ((p) => defaultFetchJSON(p));
  const postFn = opts.postFn ?? ((p) =>
    globalThis.fetch(p, { method: 'POST', credentials: 'same-origin' }));
  const toasts = opts.toasts ?? { success: () => {}, error: () => {} };
  const wait = opts.waitFn ?? defaultWait;
  const retryDelayMs = opts.retryDelayMs ?? 2000;

  async function fetchUnseen() {
    state.loading = true;
    state.error = null;
    try {
      const data = await fetchJSON('/api/release-notes/unseen');
      const filtered = filterBySchema(data?.notes);
      state.unseen = sortNotes(filtered);
      if (data?.current) state.current = data.current;
      // last_seen is only populated on the /unseen endpoint (caller-
      // specific). Omitted or empty means fresh install / never acked.
      state.lastSeen = data?.last_seen ?? '';
    } catch (e) {
      state.error = e?.message || String(e);
    } finally {
      state.loading = false;
    }
  }

  async function fetchAll() {
    state.loading = true;
    state.error = null;
    try {
      const data = await fetchJSON('/api/release-notes');
      const filtered = filterBySchema(data?.notes);
      state.all = sortNotes(filtered);
      if (data?.current) state.current = data.current;
    } catch (e) {
      state.error = e?.message || String(e);
    } finally {
      state.loading = false;
    }
  }

  // Optimistic ack. Clears `unseen` immediately so the popup can close
  // without waiting on the network. Success → muted "Saved" toast.
  // Failure → one retry after `retryDelayMs`, then a muted warning
  // toast so the operator knows the ack didn't stick.
  async function ack() {
    state.unseen = [];
    const attempt = async () => postFn('/api/release-notes/ack');
    try {
      const res = await attempt();
      if (!res || !res.ok) throw new Error(`HTTP ${res?.status ?? 'error'}`);
      toasts.success('Saved');
      return;
    } catch {
      // first failure — retry once after retryDelayMs
    }
    await wait(retryDelayMs);
    try {
      const res = await attempt();
      if (!res || !res.ok) throw new Error(`HTTP ${res?.status ?? 'error'}`);
      toasts.success('Saved');
    } catch {
      toasts.error("Couldn't save — we'll show these again next time.");
    }
  }

  return { fetchUnseen, fetchAll, ack };
}
