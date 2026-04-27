// Tests for the release-notes store. Exercises the pure-JS core in
// releaseNotesCore.js — the same engine that releaseNotesStore.svelte.js
// wraps with $state runes at runtime.
//
// Run with:
//   node --test src/lib/releaseNotesStore.test.js
//
// The file mirrors the style of stores/channelsCore.test.js and
// stores/ignoredInvites.test.js: node:test, no runtime dependencies
// beyond the module under test.

import { strict as assert } from 'node:assert';
import { describe, it, beforeEach } from 'node:test';

import {
  cmpVersion,
  sortNotes,
  filterBySchema,
  makeReleaseNotesCore,
  MAX_SCHEMA,
} from './releaseNotesCore.js';

// Helper: a mutable state POJO matching the shape the Svelte wrapper
// supplies.
function makeState() {
  return { unseen: [], all: [], loading: false, error: null, current: '' };
}

// Helper: toast recorder. Every call is captured so tests can assert
// exactly which branch fired.
function makeToasts() {
  const calls = [];
  return {
    calls,
    success: (msg) => calls.push({ level: 'success', msg }),
    error: (msg) => calls.push({ level: 'error', msg }),
  };
}

// ---------------------------------------------------------------------
// Pure helpers: cmpVersion, sortNotes, filterBySchema.
// ---------------------------------------------------------------------

describe('cmpVersion', () => {
  it('orders real x.y.z versions numerically by component', () => {
    // The client-side cmpVersion is used only for the defense-in-depth
    // sort. Filtering by lastSeen is done server-side; the client never
    // compares an empty-string version against a real one in practice,
    // so cmpVersion only needs to be correct for real-vs-real compares.
    // (The Go-side releasenotes.Compare() has a special rule that
    // Compare("", anything) == -1; the client's cmpVersion treats empty
    // as zero-valued, which is a deliberate scope reduction.)
    assert.equal(cmpVersion('0.11.0', '0.11.0'), 0);
    assert.equal(cmpVersion('0.10.11', '0.11.0'), -1);
    assert.equal(cmpVersion('0.11.0', '0.10.11'), 1);
    assert.equal(cmpVersion('1.0.0', '0.99.99'), 1);
    assert.equal(cmpVersion('0.2.0', '0.10.0'), -1);
    assert.equal(cmpVersion('0.0.0', '0.0.0'), 0);
  });

  it('strips suffixes after the first non-digit-or-dot', () => {
    // Beta and dev-dirty suffixes must collapse to the bare x.y.z so
    // client-side sort matches the server's canonical ordering.
    assert.equal(cmpVersion('0.11.0-beta.3', '0.11.0'), 0);
    assert.equal(cmpVersion('0.11.0-beta.1', '0.11.0-beta.9'), 0);
    assert.equal(cmpVersion('0.11.0-abc1234-dirty', '0.11.0'), 0);
    assert.equal(cmpVersion('0.11.0-abc1234-dirty', '0.10.11'), 1);
  });
});

describe('sortNotes', () => {
  it('puts CTAs ahead of info even when the CTA is older', () => {
    // Primary sort by style (cta first), secondary by version desc.
    const input = [
      { version: '0.12.0', style: 'info', title: 'newer info' },
      { version: '0.11.0', style: 'cta', title: 'older cta' },
    ];
    const sorted = sortNotes(input);
    assert.equal(sorted[0].style, 'cta', 'CTA must come first');
    assert.equal(sorted[0].version, '0.11.0');
    assert.equal(sorted[1].style, 'info');
  });

  it('defense-in-depth: reorders a server response that sent info ahead of cta', () => {
    // The server is supposed to pre-sort, but a stale cached response or
    // a bad backend must not let info cards appear above CTAs. The
    // store re-applies the sort.
    const input = [
      { version: '0.10.11', style: 'info', title: 'wrong-order info' },
      { version: '0.10.0', style: 'cta', title: 'should-be-first cta' },
      { version: '0.11.0', style: 'info', title: 'another info' },
      { version: '0.11.5', style: 'cta', title: 'newer cta' },
    ];
    const sorted = sortNotes(input);
    // First two must be the CTAs, in version-desc order.
    assert.equal(sorted[0].style, 'cta');
    assert.equal(sorted[0].version, '0.11.5');
    assert.equal(sorted[1].style, 'cta');
    assert.equal(sorted[1].version, '0.10.0');
    // Then the infos, in version-desc order.
    assert.equal(sorted[2].style, 'info');
    assert.equal(sorted[2].version, '0.11.0');
    assert.equal(sorted[3].style, 'info');
    assert.equal(sorted[3].version, '0.10.11');
  });

  it('tolerates null / undefined / empty input', () => {
    assert.deepEqual(sortNotes(null), []);
    assert.deepEqual(sortNotes(undefined), []);
    assert.deepEqual(sortNotes([]), []);
  });

  it('does not mutate the input array', () => {
    const input = [
      { version: '0.12.0', style: 'info' },
      { version: '0.11.0', style: 'cta' },
    ];
    const snapshot = [...input];
    sortNotes(input);
    assert.deepEqual(input, snapshot, 'input must not be mutated');
  });
});

describe('filterBySchema', () => {
  it(`drops notes with schema_version > MAX_SCHEMA (${MAX_SCHEMA})`, () => {
    // A stale SPA bundle against a newer backend silently drops
    // unknown-schema entries (plan D9 forward compatibility).
    const input = [
      { version: '0.11.0', schema_version: 1, title: 'keep' },
      { version: '0.12.0', schema_version: 2, title: 'drop — future schema' },
      { version: '0.13.0', schema_version: 3, title: 'drop — further future' },
      { version: '0.14.0', schema_version: 1, title: 'keep' },
    ];
    const filtered = filterBySchema(input);
    assert.equal(filtered.length, 2);
    assert.equal(filtered[0].version, '0.11.0');
    assert.equal(filtered[1].version, '0.14.0');
  });

  it('treats a missing schema_version as 1 (default)', () => {
    const input = [
      { version: '0.11.0', title: 'no schema_version field' },
      { version: '0.12.0', schema_version: 1, title: 'explicit 1' },
    ];
    const filtered = filterBySchema(input);
    assert.equal(filtered.length, 2);
  });

  it('tolerates null / undefined input', () => {
    assert.deepEqual(filterBySchema(null), []);
    assert.deepEqual(filterBySchema(undefined), []);
  });

  it('honors a custom max for parametric tests', () => {
    const input = [
      { version: '0.11.0', schema_version: 1 },
      { version: '0.12.0', schema_version: 2 },
      { version: '0.13.0', schema_version: 3 },
    ];
    assert.equal(filterBySchema(input, 2).length, 2);
    assert.equal(filterBySchema(input, 3).length, 3);
    assert.equal(filterBySchema(input, 0).length, 0);
  });
});

// ---------------------------------------------------------------------
// Core integration: fetchUnseen / fetchAll / ack with mocked fetch and
// mocked toaster.
// ---------------------------------------------------------------------

describe('makeReleaseNotesCore — fetchUnseen', () => {
  let state;
  let toasts;

  beforeEach(() => {
    state = makeState();
    toasts = makeToasts();
  });

  it('populates unseen with sorted + schema-filtered notes and sets current', async () => {
    const response = {
      schema_version: 1,
      current: '0.11.0',
      notes: [
        { version: '0.10.11', schema_version: 1, style: 'info', title: 'info' },
        { version: '0.11.0', schema_version: 1, style: 'cta', title: 'cta' },
        { version: '0.12.0', schema_version: 2, style: 'info', title: 'future' },
      ],
    };
    const core = makeReleaseNotesCore({
      state,
      toasts,
      fetchFn: async (path) => {
        assert.equal(path, '/api/release-notes/unseen');
        return response;
      },
    });

    await core.fetchUnseen();

    assert.equal(state.loading, false);
    assert.equal(state.error, null);
    assert.equal(state.current, '0.11.0');
    // Future-schema note dropped; CTA first.
    assert.equal(state.unseen.length, 2);
    assert.equal(state.unseen[0].style, 'cta');
    assert.equal(state.unseen[0].version, '0.11.0');
    assert.equal(state.unseen[1].style, 'info');
  });

  it('populates error and leaves loading=false on a rejecting fetch', async () => {
    const core = makeReleaseNotesCore({
      state,
      toasts,
      fetchFn: async () => { throw new Error('network down'); },
    });

    await core.fetchUnseen();

    assert.equal(state.loading, false);
    assert.equal(state.error, 'network down');
    assert.deepEqual(state.unseen, []);
  });

  it('leaves state.current untouched when the response omits current', async () => {
    state.current = '0.11.0';
    const core = makeReleaseNotesCore({
      state,
      toasts,
      fetchFn: async () => ({ notes: [] }),
    });

    await core.fetchUnseen();

    assert.equal(state.current, '0.11.0', 'current must not be wiped');
  });
});

describe('makeReleaseNotesCore — fetchAll', () => {
  let state;
  let toasts;

  beforeEach(() => {
    state = makeState();
    toasts = makeToasts();
  });

  it('fetches from /api/release-notes and populates state.all', async () => {
    const response = {
      schema_version: 1,
      current: '0.11.0',
      notes: [
        { version: '0.10.11', schema_version: 1, style: 'info', title: 'I' },
        { version: '0.11.0', schema_version: 1, style: 'cta', title: 'C' },
      ],
    };
    const core = makeReleaseNotesCore({
      state,
      toasts,
      fetchFn: async (path) => {
        assert.equal(path, '/api/release-notes');
        return response;
      },
    });

    await core.fetchAll();

    assert.equal(state.loading, false);
    assert.equal(state.error, null);
    assert.equal(state.all.length, 2);
    assert.equal(state.all[0].style, 'cta');
  });

  it('populates error on fetch rejection', async () => {
    const core = makeReleaseNotesCore({
      state,
      toasts,
      fetchFn: async () => { throw new Error('oops'); },
    });

    await core.fetchAll();

    assert.equal(state.error, 'oops');
    assert.equal(state.loading, false);
    assert.deepEqual(state.all, []);
  });
});

describe('makeReleaseNotesCore — ack', () => {
  let state;
  let toasts;

  beforeEach(() => {
    state = makeState();
    state.unseen = [
      { version: '0.11.0', style: 'cta', title: 'a' },
      { version: '0.10.11', style: 'info', title: 'b' },
    ];
    toasts = makeToasts();
  });

  it('optimistically clears unseen synchronously (before network resolves)', async () => {
    let resolveFetch;
    const ackCalls = [];
    const core = makeReleaseNotesCore({
      state,
      toasts,
      postFn: (path) => {
        ackCalls.push(path);
        return new Promise((r) => { resolveFetch = r; });
      },
      waitFn: async () => {}, // no-op
    });

    const p = core.ack();

    // After kicking off ack() but BEFORE awaiting, unseen must be [].
    // ack() is async but the optimistic clear happens synchronously at
    // the top of the function body.
    assert.deepEqual(state.unseen, [], 'unseen must clear synchronously');
    assert.equal(ackCalls.length, 1, 'POST fires immediately');
    assert.equal(ackCalls[0], '/api/release-notes/ack');

    // Resolve the network and let ack() finish.
    resolveFetch({ ok: true, status: 204 });
    await p;

    // Success toast fired.
    assert.equal(toasts.calls.length, 1);
    assert.equal(toasts.calls[0].level, 'success');
    assert.equal(toasts.calls[0].msg, 'Saved');
  });

  it('retries once on first failure and fires success toast on retry success', async () => {
    let attempts = 0;
    const waitCalls = [];
    const core = makeReleaseNotesCore({
      state,
      toasts,
      postFn: async () => {
        attempts += 1;
        if (attempts === 1) return { ok: false, status: 500 };
        return { ok: true, status: 204 };
      },
      waitFn: async (ms) => { waitCalls.push(ms); },
      retryDelayMs: 2000,
    });

    await core.ack();

    assert.equal(attempts, 2, 'retry must fire exactly once');
    assert.deepEqual(waitCalls, [2000], 'backoff must be ~2s before retry');
    assert.equal(toasts.calls.length, 1);
    assert.equal(toasts.calls[0].level, 'success');
  });

  it('shows the fallback warning toast when both attempts fail', async () => {
    let attempts = 0;
    const waitCalls = [];
    const core = makeReleaseNotesCore({
      state,
      toasts,
      postFn: async () => {
        attempts += 1;
        return { ok: false, status: 503 };
      },
      waitFn: async (ms) => { waitCalls.push(ms); },
      retryDelayMs: 2000,
    });

    await core.ack();

    assert.equal(attempts, 2, 'exactly two POST attempts');
    assert.deepEqual(waitCalls, [2000]);
    assert.equal(toasts.calls.length, 1);
    assert.equal(toasts.calls[0].level, 'error');
    assert.match(
      toasts.calls[0].msg,
      /Couldn't save — we'll show these again next time/,
    );
    // unseen stays cleared — the optimistic clear persists even on
    // failure; the operator will see the popup again on next fetch
    // because the server didn't ack.
    assert.deepEqual(state.unseen, []);
  });

  it('treats a throwing fetch the same as an error response', async () => {
    let attempts = 0;
    const core = makeReleaseNotesCore({
      state,
      toasts,
      postFn: async () => {
        attempts += 1;
        throw new Error('network dropped');
      },
      waitFn: async () => {},
      retryDelayMs: 2000,
    });

    await core.ack();

    assert.equal(attempts, 2, 'thrown error on first call still triggers retry');
    assert.equal(toasts.calls[0].level, 'error');
  });

  it('does not emit both success and error toasts in any path', async () => {
    // Defense against a future regression where both branches fire.
    let attempts = 0;
    const core = makeReleaseNotesCore({
      state,
      toasts,
      postFn: async () => {
        attempts += 1;
        return { ok: attempts === 2, status: attempts === 2 ? 204 : 500 };
      },
      waitFn: async () => {},
    });

    await core.ack();

    // Exactly one toast must fire per ack() call — never two.
    assert.equal(toasts.calls.length, 1);
  });
});
