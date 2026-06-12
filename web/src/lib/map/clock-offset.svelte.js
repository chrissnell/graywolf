// Server-clock offset tracking for the live map (GH #234).
//
// graywolf stamps every packet's receive time (last_heard) and all of its
// other timestamps using the *host* clock. The browser must therefore
// measure packet age against that same host clock, not its own — otherwise a
// host whose clock is unsynced (a Pi with no RTC, or a browser that's been
// off NTP) makes ages go negative or hides stations from the map entirely.
//
// Every /api/stations response already carries a standard HTTP `Date:` header
// stamped by the host, so we derive the offset from it on each poll:
//   offsetMs = serverNow - browserNow
// and read serverNow() ≈ Date.now() + offsetMs everywhere the map computes
// packet age. No new endpoint and no protocol change — the timestamp is
// already on the wire. Round-trip latency is sub-second and the Date header
// is 1-second resolution; both are negligible against the minutes-to-hours
// skew this targets.
//
// The pure header/offset math lives in clock-offset-core.js so it can be
// unit-tested without the Svelte compiler; this file is just the reactive
// $state wrapper around it.

import { isSignificantOffset, offsetFromHeaders } from './clock-offset-core.js';

export { formatOffsetMagnitude } from './clock-offset-core.js';

export const clockOffset = (() => {
  let offsetMs = $state(0);
  let known = $state(false);

  // observe reads the host clock from a response's Date header and refreshes
  // the offset. Safe to call on every response (200, 304, even errors) — the
  // header is present regardless, so the offset stays fresh between full
  // reloads. Cached responses (those carrying an Age header) are ignored so a
  // stale stored Date can't poison the offset.
  function observe(headers) {
    const next = offsetFromHeaders(headers, Date.now());
    if (next === null) return;
    offsetMs = next;
    known = true;
  }

  return {
    observe,
    // serverNow: the browser's best estimate of the graywolf host clock now.
    // Use this instead of Date.now() for any packet-age math.
    serverNow() { return Date.now() + offsetMs; },
    get offsetMs() { return offsetMs; },
    get known() { return known; },
    // True only once we've seen a host timestamp and it differs enough to
    // matter to the operator.
    get isSignificant() { return known && isSignificantOffset(offsetMs); },
  };
})();
