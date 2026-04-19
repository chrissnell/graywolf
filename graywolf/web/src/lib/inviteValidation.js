// Pure-logic helpers for the invite-to-tactical modal.
//
// Extracted so they can be unit-tested without a Svelte runtime. The
// modal composes these into its stateful chip flow; the tests exercise
// each rule in isolation.

// Matches the APRS callsign syntax that both `ParseInvite` (the backend
// wire parser) and the `tactical_callsign` column accept. Uppercase-only;
// the caller must normalize before matching.
export const CALLSIGN_RE = /^[A-Z0-9-]{1,9}$/;

// Paste separators: comma, semicolon, any whitespace.
export const PASTE_SPLIT_RE = /[,\s;]+/;

/** Normalize a raw input token: trim + uppercase. */
export function normalizeCall(raw) {
  return (raw || '').trim().toUpperCase();
}

/** Validate a *normalized* call against the APRS regex. */
export function isValidCall(call) {
  return CALLSIGN_RE.test(call || '');
}

/**
 * Split a pasted blob into an array of normalized call tokens (empties
 * dropped). Does NOT validate — caller filters via isValidCall.
 */
export function splitPasteList(pasted) {
  if (!pasted) return [];
  return pasted
    .split(PASTE_SPLIT_RE)
    .map(normalizeCall)
    .filter(Boolean);
}

/**
 * Classify a single commit attempt against an existing chip set + the
 * operator's own callsign. Returns one of:
 *   'invalid'   — token fails CALLSIGN_RE
 *   'self'      — token equals the operator's callsign
 *   'duplicate' — token already in `existingCalls`
 *   'ok'        — safe to add as a new chip
 * The caller owns the actual mutation; this function is side-effect free.
 */
export function classifyCommit(rawToken, existingCalls, ownCallsign) {
  const call = normalizeCall(rawToken);
  if (!call) return 'invalid';
  if (!isValidCall(call)) return 'invalid';
  const own = normalizeCall(ownCallsign);
  if (own && call === own) return 'self';
  for (const existing of existingCalls) {
    if (normalizeCall(existing) === call) return 'duplicate';
  }
  return 'ok';
}

/**
 * Run a paste-list classification pass. Returns the batched result so
 * the UI can decide what to show ("3 added · 1 invalid"). `existingCalls`
 * grows as valid tokens are "committed" within the same paste (so a
 * pasted list with A, A, B yields 2 added + 1 duplicate, not 2 added).
 *
 * @param {string} pasted
 * @param {Iterable<string>} existingCalls
 * @param {string} ownCallsign
 * @returns {{added: string[], duplicate: string[], invalid: string[], self: string[]}}
 */
export function classifyPasteList(pasted, existingCalls, ownCallsign) {
  const seen = new Set([...existingCalls].map(normalizeCall));
  const result = { added: [], duplicate: [], invalid: [], self: [] };
  for (const tok of splitPasteList(pasted)) {
    const outcome = classifyCommit(tok, seen, ownCallsign);
    switch (outcome) {
      case 'ok':
        result.added.push(tok);
        seen.add(tok);
        break;
      case 'self':
        result.self.push(tok);
        break;
      case 'duplicate':
        result.duplicate.push(tok);
        break;
      default:
        result.invalid.push(tok);
    }
  }
  return result;
}
