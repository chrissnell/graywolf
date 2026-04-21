// Tests for channelsCore — the pure-JS polling / invalidate engine
// underpinning the Svelte 5 `channelsStore`.
//
// Run with:
//   node --test src/lib/stores/channelsCore.test.js
//
// The file mirrors the style of ignoredInvites.test.js: node:test,
// no runtime dependencies beyond the module under test.

import { strict as assert } from 'node:assert';
import { describe, it, beforeEach } from 'node:test';

import { makeChannelsCore } from './channelsCore.js';

function makeState() {
  return { list: [], loading: true, error: null, lastUpdated: null };
}

describe('channelsCore', () => {
  let state;

  beforeEach(() => {
    state = makeState();
  });

  it('invalidate() populates state.list and clears loading', async () => {
    const rows = [
      { id: 1, name: 'VHF', backing: { summary: 'modem', health: 'live' } },
    ];
    const core = makeChannelsCore({
      state,
      fetchFn: async () => rows,
      skipFocusListener: true,
    });

    await core.invalidate();

    assert.equal(state.loading, false);
    assert.equal(state.error, null);
    assert.deepEqual(state.list, rows);
    assert.ok(state.lastUpdated != null, 'lastUpdated should be set');
  });

  it('invalidate() surfaces fetch errors without clobbering list', async () => {
    let first = true;
    const core = makeChannelsCore({
      state,
      fetchFn: async () => {
        if (first) {
          first = false;
          return [{ id: 1, name: 'VHF' }];
        }
        throw new Error('boom');
      },
      skipFocusListener: true,
    });

    await core.invalidate(); // success
    await core.invalidate(); // error

    assert.equal(state.error, 'boom');
    // The prior list is preserved — UI keeps rendering the last known
    // state rather than going blank on a transient failure.
    assert.deepEqual(state.list, [{ id: 1, name: 'VHF' }]);
  });

  it('overlapping invalidate() calls coalesce', async () => {
    let calls = 0;
    let resolveFetch;
    const core = makeChannelsCore({
      state,
      fetchFn: () =>
        new Promise((r) => {
          calls += 1;
          resolveFetch = r;
        }),
      skipFocusListener: true,
    });

    const p1 = core.invalidate();
    const p2 = core.invalidate();
    const p3 = core.invalidate();
    assert.equal(calls, 1, 'only one fetch should be in flight');
    assert.strictEqual(p1, p2);
    assert.strictEqual(p2, p3);

    resolveFetch([]);
    await p1;
    assert.equal(calls, 1);
  });

  it('start() kicks an initial fetch and schedules the interval', async () => {
    let calls = 0;
    const core = makeChannelsCore({
      state,
      fetchFn: async () => {
        calls += 1;
        return [];
      },
      skipFocusListener: true,
      defaultPollMs: 10,
    });

    core.start();
    // Give the microtask queue a turn so the initial await resolves.
    await new Promise((r) => setTimeout(r, 0));
    assert.equal(calls, 1, 'initial fetch should have run');

    // Wait long enough for at least one interval tick.
    await new Promise((r) => setTimeout(r, 25));
    core.stop();

    assert.ok(calls >= 2, `expected polling tick, got calls=${calls}`);
  });

  it('stop() cancels the scheduled tick', async () => {
    let calls = 0;
    const core = makeChannelsCore({
      state,
      fetchFn: async () => {
        calls += 1;
        return [];
      },
      skipFocusListener: true,
      defaultPollMs: 5,
    });

    core.start();
    await new Promise((r) => setTimeout(r, 0));
    const callsAtStop = calls;
    core.stop();
    await new Promise((r) => setTimeout(r, 30));
    assert.equal(
      calls,
      callsAtStop,
      'no further fetches should fire after stop()',
    );
  });

  it('start() is idempotent', async () => {
    let calls = 0;
    const core = makeChannelsCore({
      state,
      fetchFn: async () => {
        calls += 1;
        return [];
      },
      skipFocusListener: true,
      defaultPollMs: 1_000,
    });

    core.start();
    core.start();
    core.start();
    await new Promise((r) => setTimeout(r, 0));
    core.stop();
    assert.equal(calls, 1, 'second start() must not fire a second fetch');
  });
});
