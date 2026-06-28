// Pure logic for detecting that the graywolf server changed underneath an
// open browser tab (the operator upgraded/rebuilt graywolf). Kept free of
// Svelte runes, fetch, and timers so it runs under `node --test`; the
// reactive glue (fetch + interval + connection store) lives in
// stores/server-version.svelte.js.

// Separator joining version + commit into one identity token. A pipe can't
// appear in a semver-ish version string or a git hash, so two different
// (version, commit) pairs can never alias to the same token.
const SEP = '|';

// serverIdentity reduces a GET /api/version response to a single token that
// uniquely names the running build: the (version, commit) pair. Comparing
// the commit too — not just the release version — means a same-version
// rebuild from source is still detected. Returns '' for a missing/blank
// response so a transient failure is treated as "no information," never as
// a change.
export function serverIdentity(data) {
  if (!data || typeof data !== 'object') return '';
  const version = typeof data.version === 'string' ? data.version : '';
  const commit = typeof data.commit === 'string' ? data.commit : '';
  if (version === '' && commit === '') return '';
  return version + SEP + commit;
}

// createIdentityWatcher is a one-way latch. It records the first non-empty
// identity it sees (the build the page was loaded against) and reports a
// change once it later observes a *different* non-empty identity. Empty
// observations are ignored, so neither a failed fetch nor a server that
// reports no version can establish or move the baseline, or false-trigger.
// Once changed it stays changed — the operator should reload regardless of
// any later flapping.
export function createIdentityWatcher() {
  let boot = '';
  let changed = false;
  return {
    get boot() { return boot; },
    get changed() { return changed; },
    // observe folds one identity into the latch and returns true only on
    // the observation that flips changed false -> true (so a caller can
    // act exactly once).
    observe(identity) {
      if (!identity) return false;
      if (boot === '') {
        boot = identity;
        return false;
      }
      if (!changed && identity !== boot) {
        changed = true;
        return true;
      }
      return false;
    },
  };
}
