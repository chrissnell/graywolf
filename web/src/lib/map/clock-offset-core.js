// Pure clock-offset logic for the live map (GH #234), separated from the
// Svelte reactive wrapper in clock-offset.svelte.js so it can be unit-tested
// under plain `node --test` (the wrapper uses $state runes that need the
// Svelte compiler).

// Below this magnitude the difference is just round-trip / 1-second header
// resolution noise rather than a real clock disagreement; don't surface it.
export const SIGNIFICANT_MS = 2_000;

// Compact, human magnitude for an offset (sign handled by the caller).
export function formatOffsetMagnitude(ms) {
  const s = Math.round(Math.abs(ms) / 1000);
  if (s < 90) return `${s}s`;
  const m = Math.round(s / 60);
  if (m < 90) return `${m}m`;
  return `${Math.round(m / 60)}h`;
}

export function isSignificantOffset(offsetMs) {
  return Math.abs(offsetMs) >= SIGNIFICANT_MS;
}

// offsetFromHeaders derives offsetMs = serverNow - browserNow from a response's
// host-stamped `Date:` header, or returns null when the response should not
// update the offset. browserNowMs is injected so the math is deterministic in
// tests.
//
// Cached responses are rejected: an HTTP cache MUST stamp an `Age:` header, and
// such a response's `Date:` reflects when it was stored at the origin, not now —
// trusting it would poison the offset by exactly the cache residence time.
export function offsetFromHeaders(headers, browserNowMs) {
  if (!headers || typeof headers.get !== 'function') return null;

  const age = headers.get('Age');
  if (age != null && age !== '' && Number(age) !== 0) return null;

  const dateHdr = headers.get('Date');
  if (!dateHdr) return null;
  const serverMs = Date.parse(dateHdr);
  if (Number.isNaN(serverMs)) return null;

  return serverMs - browserNowMs;
}
