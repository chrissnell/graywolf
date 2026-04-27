// Client-side helpers for reasoning about the station callsign. The
// backend (`pkg/callsign`) is the canonical source of truth; this file
// only exists so settings pages can render the "callsign unset" banner
// without a round-trip. Keep the logic in lock-step with
// pkg/callsign/parse.go `IsN0Call`.

// isN0Call returns true for the literal "N0CALL" base callsign, with or
// without an SSID suffix, case-insensitive. Whitespace is trimmed. An
// empty string is NOT N0CALL (use isStationCallsignMissing for the
// combined "empty or N0CALL" test).
export function isN0Call(value) {
  if (typeof value !== 'string') return false;
  const trimmed = value.trim();
  if (trimmed === '') return false;
  const upper = trimmed.toUpperCase();
  const dash = upper.indexOf('-');
  const base = dash === -1 ? upper : upper.slice(0, dash);
  return base === 'N0CALL';
}

// isStationCallsignMissing is the UI-level predicate the iGate and
// Digipeater pages use to decide whether to render the warning banner
// and aria-disable the Enable toggle. True when the stored value is
// empty OR an N0CALL variant.
export function isStationCallsignMissing(value) {
  if (typeof value !== 'string') return true;
  if (value.trim() === '') return true;
  return isN0Call(value);
}
