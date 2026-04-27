// List-card breakage indicator helper for beacon / iGate / digipeater
// referrers. Phase 3, plan D4.
//
// Every referrer row (a beacon, an iGate TX channel, a digipeater
// rule's from/to channel) points at a channel by numeric FK. This
// helper classifies that reference into one of three bucket states so
// the list-card pill can render a single source of truth:
//
//   'ok'          -- the channel exists and is TX-capable.
//   'unreachable' -- the channel exists but its current backing
//                    cannot transmit (modem missing I/O device, or
//                    other reason surfaced on backing.tx.reason).
//   'deleted'     -- the channel referenced by the FK is not present
//                    in the shared channelsStore at all (orphaned
//                    soft-FK; see plan D5).
//
// The status is consumed by list-card rendering only. Forms use the
// Phase 2 txPredicate directly against a known-selected channel; the
// "deleted" case cannot arise there because the picker is sourced
// from the same channelsStore list.
//
// Polled-store lag is acceptable: the pill reflects the last polled
// channel state. The authoritative gate on save is the server's
// 400/409 (see plan "Risks & non-goals"). This helper has no
// dependency on the form layer.

import { TX_REASON_FALLBACK } from './channelBacking.js';

/**
 * @typedef {Object} ChannelRefStatus
 * @property {'ok'|'unreachable'|'deleted'} status
 * @property {string} reason
 *   Short human string. Non-empty iff status !== 'ok'. For the
 *   'deleted' case it's a fixed 'channel deleted' string so renderers
 *   can treat it uniformly; for 'unreachable' it's whatever
 *   `backing.tx.reason` the server surfaced (typically the
 *   `TX_REASON_NO_INPUT_DEVICE` / `TX_REASON_NO_OUTPUT_DEVICE`
 *   constants exported from channelBacking.js).
 * @property {object|null} channel
 *   The resolved channel object from the store, or null when the
 *   FK is orphaned. Callers that need the channel name for display
 *   can reach for this rather than a second lookup.
 */

/**
 * Status constants. Three concrete string values; stable wire-style
 * tokens so tests and downstream renderers can switch on them
 * without string-matching anywhere.
 */
export const STATUS_OK = 'ok';
export const STATUS_UNREACHABLE = 'unreachable';
export const STATUS_DELETED = 'deleted';

/**
 * Fixed reason string for the orphan case. The caller is responsible
 * for rendering the numeric FK in aria-label / title only -- per D4,
 * the visible pill label should NOT leak `#N` because operators who
 * named their channels don't want to see integer FKs.
 */
export const REASON_DELETED = 'channel deleted';

/**
 * Classify a referrer's channel reference against the currently-known
 * channel set.
 *
 * @param {number|null|undefined} channelId
 *   Numeric FK from the referrer row (e.g. `beacon.channel`,
 *   `igate.tx_channel`, `rule.from_channel`).
 * @param {Map<number, object>} channelsById
 *   Map keyed by numeric channel id. Callers typically build this
 *   via `$derived` over `channelsStore.list`.
 * @returns {ChannelRefStatus}
 */
export function channelRefStatus(channelId, channelsById) {
  // Coerce to number so callers passing either string or numeric FKs
  // (both exist in the codebase) get a consistent map lookup.
  const id = typeof channelId === 'string' ? parseInt(channelId, 10) : channelId;
  if (id == null || !Number.isFinite(id) || id <= 0) {
    // Treat zero / negative / non-numeric IDs as orphan. The store's
    // channels all have positive integer ids, so anything else can't
    // match.
    return { status: STATUS_DELETED, reason: REASON_DELETED, channel: null };
  }
  const channel = channelsById instanceof Map ? channelsById.get(id) : undefined;
  if (!channel) {
    return { status: STATUS_DELETED, reason: REASON_DELETED, channel: null };
  }
  if (!channel.backing?.tx?.capable) {
    const reason = channel.backing?.tx?.reason || TX_REASON_FALLBACK;
    return { status: STATUS_UNREACHABLE, reason, channel };
  }
  return { status: STATUS_OK, reason: '', channel };
}

/**
 * Build a `Map<number, Channel>` from an array of channel records.
 * Thin convenience so each caller doesn't reimplement the same
 * reducer inside a `$derived`.
 */
export function buildChannelsById(channels) {
  const m = new Map();
  if (!Array.isArray(channels)) return m;
  for (const c of channels) {
    if (c && typeof c.id === 'number') m.set(c.id, c);
  }
  return m;
}
