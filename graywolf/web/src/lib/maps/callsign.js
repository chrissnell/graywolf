// graywolf/web/src/lib/maps/callsign.js
//
// Pure-function callsign normalizer that mirrors the server-side regex
// (^[A-Z0-9]{3,9}$, must contain a digit, SSID stripped). Used by the
// Maps Settings form for immediate validation feedback before hitting
// the wire — server is still authoritative.

const CALLSIGN_RE = /^[A-Z0-9]{3,9}$/;

export function normalizeCallsign(input) {
  const upper = String(input ?? '').trim().toUpperCase();
  const idx = upper.indexOf('-');
  const base = idx >= 0 ? upper.slice(0, idx) : upper;
  return base;
}

export function validateCallsign(input) {
  const cs = normalizeCallsign(input);
  if (!CALLSIGN_RE.test(cs)) {
    return { ok: false, message: 'Callsign must be 3-9 letters and digits.' };
  }
  if (!/[0-9]/.test(cs)) {
    return { ok: false, message: 'Callsign must contain at least one digit.' };
  }
  return { ok: true, callsign: cs };
}
