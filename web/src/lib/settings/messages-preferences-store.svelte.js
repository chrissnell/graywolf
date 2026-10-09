// Reactive messages-preferences store, backed by the server.
// Mirrors the pattern of units-store.svelte.js: singleton IIFE, Svelte 5
// runes for reactive state, getters for read-only exposure. Any component
// in the tree (settings page, ComposeBar, sent-message rows) that imports
// `messagesPreferencesState` observes the same state and re-renders when
// it changes — no events, no prop drilling.
//
// Field shape mirrors the backend DTO (see pkg/webapi/dto/messages.go):
//   default_path?: string
//   fallback_policy?: string
//   retention_days?: number
//   retry_max_attempts?: number
//   max_message_text_override?: uint32  // 0 = default enforce 67; 68..200 = override
//
// The backend normalizes corrupt stored values to 0 on read, so the store
// assumes a well-formed response. For safety we still clamp unknown
// positive values into [68, 200] before exposing them, and fall back to 0
// (default enforced) on GET failures so the compose bar errs on the safe
// side.
//
// Propagation for Phase 3 (ComposeBar reactive limit): the ComposeBar
// should import this store and read `.maxMessageText` inside a derived
// expression. No fetch needed in the ComposeBar — the settings page loads
// the store on mount, and a successful PUT from the settings page updates
// the state synchronously, which re-drives any subscribers in the next
// Svelte tick.

import { toasts } from '../stores.js';
import { getPreferences, putPreferences } from '../../api/messages.js';

const DEFAULT_CAP = 67;
const UNSAFE_CAP = 200;

const VALID_FALLBACK_POLICIES = new Set(['rf_only', 'is_fallback', 'is_only', 'both']);

// Normalize a value from the wire into a valid override:
//   0          -> 0 (default enforced)
//   68..200    -> itself (raised cap)
//   anything else -> 0 (treat as default)
function normalizeOverride(v) {
  if (typeof v !== 'number' || !Number.isFinite(v)) return 0;
  if (v === 0) return 0;
  if (v >= 68 && v <= UNSAFE_CAP) return Math.floor(v);
  return 0;
}

const DEFAULT_RETRY_MAX_ATTEMPTS = 4;
const DEFAULT_RETRY_INTERVAL_SECS = 30;

// Build the full preferences PUT payload from a hydrated baseline plus a
// per-setter patch. Fields are enumerated explicitly so adding a new field
// forces every setter to opt in rather than silently inheriting via spread.
// Always normalizes max_message_text_override.
function buildPayload(baseline, patch) {
  const merged = { ...baseline, ...patch };
  return {
    default_path: merged.default_path,
    fallback_policy: merged.fallback_policy,
    retention_days: merged.retention_days,
    retry_max_attempts: merged.retry_max_attempts,
    retry_interval_secs: merged.retry_interval_secs,
    max_message_text_override: normalizeOverride(merged.max_message_text_override),
  };
}

export const messagesPreferencesState = (() => {
  // Full preferences payload, so PUTs can round-trip sibling fields
  // without dropping them. Populated by fetchPreferences; `null` until
  // first successful load.
  let prefs = $state(/** @type {null | {
    default_path?: string,
    fallback_policy?: string,
    retention_days?: number,
    retry_max_attempts?: number,
    retry_interval_secs?: number,
    max_message_text_override?: number,
  }} */ (null));

  let loaded = $state(false);
  // hydrated is true only after a GET that returned a real server row.
  // `loaded` can be true while `hydrated` is false when the initial GET
  // failed and we fell back to a read-only default — in that state we
  // must refuse to PUT, because the baseline we'd merge against is the
  // tiny `{ max_message_text_override: 0 }` stub and PUTting that would
  // wipe default_path / retry_max_attempts / retention_days on the row.
  let hydrated = $state(false);
  let saving = $state(false);
  let error = $state(/** @type {string | null} */ (null));

  async function fetchPreferences() {
    try {
      const data = await getPreferences();
      const next = data ?? {};
      next.max_message_text_override = normalizeOverride(next.max_message_text_override);
      prefs = next;
      loaded = true;
      hydrated = true;
      error = null;
      return next;
    } catch (e) {
      error = e?.message || String(e);
      // On failure, expose a best-effort default so readers (e.g. the
      // compose bar) keep enforcing the safe 67-char cap. `hydrated`
      // stays false so setOverride refuses to PUT until a real GET lands.
      if (!prefs) prefs = { max_message_text_override: 0 };
      loaded = true;
      return prefs;
    }
  }

  // Set the override, preserving all other fields on the wire. Optimistic:
  // apply locally, PUT, rollback on failure.
  async function setOverride(nextOverride) {
    const normalized = normalizeOverride(nextOverride);
    // Refuse to PUT against a stub baseline — re-fetch first so sibling
    // fields (default_path, retry_max_attempts, retention_days) survive
    // the write.
    if (!hydrated) {
      await fetchPreferences();
      if (!hydrated) {
        toasts.error("Couldn't load preferences — try again in a moment.");
        return;
      }
    }
    const baseline = prefs;
    const prev = baseline;
    const optimistic = { ...baseline, max_message_text_override: normalized };
    prefs = optimistic;
    saving = true;
    error = null;
    try {
      const resp = await putPreferences(buildPayload(baseline, { max_message_text_override: normalized }));
      const next = resp ?? optimistic;
      next.max_message_text_override = normalizeOverride(next.max_message_text_override);
      prefs = next;
      toasts.success('Saved');
    } catch (e) {
      prefs = prev;
      error = e?.message || String(e);
      toasts.error("Couldn't save message preferences — try again.");
    } finally {
      saving = false;
    }
  }

  // Toggle-friendly setter: false -> 0 (default enforced), true -> 200.
  async function setAllowLong(allow) {
    return setOverride(allow ? UNSAFE_CAP : 0);
  }

  async function setFallbackPolicy(policy) {
    if (!VALID_FALLBACK_POLICIES.has(policy)) return;
    if (!hydrated) {
      await fetchPreferences();
      if (!hydrated) {
        toasts.error("Couldn't load preferences — try again in a moment.");
        return;
      }
    }
    if (prefs?.fallback_policy === policy) return;
    const baseline = prefs;
    const prev = baseline;
    const optimistic = { ...baseline, fallback_policy: policy };
    prefs = optimistic;
    saving = true;
    error = null;
    try {
      const resp = await putPreferences(buildPayload(baseline, { fallback_policy: policy }));
      const next = resp ?? optimistic;
      next.max_message_text_override = normalizeOverride(next.max_message_text_override);
      prefs = next;
      toasts.success('Saved');
    } catch (e) {
      prefs = prev;
      error = e?.message || String(e);
      toasts.error("Couldn't save fallback policy — try again.");
    } finally {
      saving = false;
    }
  }

  async function setRetryMaxAttempts(n) {
    const v = Math.max(1, Math.min(20, Math.floor(Number(n))));
    if (!hydrated) {
      await fetchPreferences();
      if (!hydrated) {
        toasts.error("Couldn't load preferences — try again in a moment.");
        return;
      }
    }
    if (prefs?.retry_max_attempts === v) return;
    const baseline = prefs;
    const prev = baseline;
    prefs = { ...baseline, retry_max_attempts: v };
    saving = true;
    error = null;
    try {
      const resp = await putPreferences(buildPayload(baseline, { retry_max_attempts: v }));
      prefs = resp ?? prefs;
      toasts.success('Saved');
    } catch (e) {
      prefs = prev;
      error = e?.message || String(e);
      toasts.error("Couldn't save retry attempts — try again.");
    } finally {
      saving = false;
    }
  }

  async function setRetryIntervalSecs(n) {
    const v = Math.max(10, Math.min(120, Math.floor(Number(n))));
    if (!hydrated) {
      await fetchPreferences();
      if (!hydrated) {
        toasts.error("Couldn't load preferences — try again in a moment.");
        return;
      }
    }
    if (prefs?.retry_interval_secs === v) return;
    const baseline = prefs;
    const prev = baseline;
    prefs = { ...baseline, retry_interval_secs: v };
    saving = true;
    error = null;
    try {
      const resp = await putPreferences(buildPayload(baseline, { retry_interval_secs: v }));
      prefs = resp ?? prefs;
      toasts.success('Saved');
    } catch (e) {
      prefs = prev;
      error = e?.message || String(e);
      toasts.error("Couldn't save retry interval — try again.");
    } finally {
      saving = false;
    }
  }

  async function setDefaultPath(path) {
    const v = (path ?? '').trim();
    if (!hydrated) {
      await fetchPreferences();
      if (!hydrated) {
        toasts.error("Couldn't load preferences — try again in a moment.");
        return;
      }
    }
    if (prefs?.default_path === v) return;
    const baseline = prefs;
    const prev = baseline;
    prefs = { ...baseline, default_path: v };
    saving = true;
    error = null;
    try {
      const resp = await putPreferences(buildPayload(baseline, { default_path: v }));
      prefs = resp ?? prefs;
      toasts.success('Saved');
    } catch (e) {
      prefs = prev;
      error = e?.message || String(e);
      toasts.error("Couldn't save digipeater path — try again.");
    } finally {
      saving = false;
    }
  }

  return {
    get loaded() { return loaded; },
    get saving() { return saving; },
    get error() { return error; },
    // Full preferences payload (read-only view).
    get prefs() { return prefs; },
    // Raw override value (0 or 68..200).
    get override() {
      return normalizeOverride(prefs?.max_message_text_override);
    },
    // Boolean projection for the settings toggle.
    get allowLong() {
      return normalizeOverride(prefs?.max_message_text_override) > 0;
    },
    get fallbackPolicy() { return prefs?.fallback_policy || 'is_fallback'; },
    get retryMaxAttempts() { return prefs?.retry_max_attempts ?? DEFAULT_RETRY_MAX_ATTEMPTS; },
    get retryIntervalSecs() { return prefs?.retry_interval_secs ?? DEFAULT_RETRY_INTERVAL_SECS; },
    get defaultPath() { return prefs?.default_path ?? 'WIDE1-1,WIDE2-1'; },
    // Effective cap the compose bar should enforce. Mirrors the server's
    // sender gate: 0 => 67, otherwise => the override value itself.
    get maxMessageText() {
      const ov = normalizeOverride(prefs?.max_message_text_override);
      return ov === 0 ? DEFAULT_CAP : ov;
    },

    fetchPreferences,
    setOverride,
    setAllowLong,
    setFallbackPolicy,
    setRetryMaxAttempts,
    setRetryIntervalSecs,
    setDefaultPath,
  };
})();

// Re-export the constants so the ComposeBar and tests can reference the
// same source-of-truth numbers without re-declaring them.
export const DEFAULT_MAX_MESSAGE_TEXT = DEFAULT_CAP;
export const MAX_MESSAGE_TEXT_CEILING = UNSAFE_CAP;
