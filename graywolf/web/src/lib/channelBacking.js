// Presentation helpers for the `backing` object returned by
// /api/channels. Every picker, card, and save-form warning routes
// through these so the glyph, label, and aria-text are consistent
// across the app.
//
// Design decisions: D7 (computed backing object), D8 (3 distinct
// glyphs + text), D17 (unbound-channel warning copy).

// Unicode glyphs chosen for shape distinctness (WCAG 1.4.1 -- not
// colour alone). Filled circle / hollow circle / em-dash.
export const GLYPH_LIVE = '●'; //  ●
export const GLYPH_DOWN = '○'; //  ○
export const GLYPH_UNBOUND = '—'; //  —

export const HEALTH_LIVE = 'live';
export const HEALTH_DOWN = 'down';
export const HEALTH_UNBOUND = 'unbound';

export const SUMMARY_MODEM = 'modem';
export const SUMMARY_KISS_TNC = 'kiss-tnc';

// Map each health value to its glyph and short user-facing text. The
// text always renders alongside the glyph (D8) -- glyph-only would
// fail a screen-reader sweep.
export function healthGlyph(health) {
  switch (health) {
    case HEALTH_LIVE:
      return GLYPH_LIVE;
    case HEALTH_DOWN:
      return GLYPH_DOWN;
    default:
      return GLYPH_UNBOUND;
  }
}

export function healthText(health) {
  switch (health) {
    case HEALTH_LIVE:
      return 'Live';
    case HEALTH_DOWN:
      return 'Backend down';
    default:
      return 'Unbound';
  }
}

// Human label for the summary line under a channel name, e.g.
//   "Modem"                          when summary=modem
//   "KISS-TNC: loramod"              single attached TNC iface
//   "KISS-TNC: loramod, radiolink"   multiple TNCs on one channel
//   "Unbound"                        summary=unbound
export function summaryLabel(backing) {
  if (!backing) return 'Unknown';
  if (backing.summary === SUMMARY_MODEM) return 'Modem';
  if (backing.summary === SUMMARY_KISS_TNC) {
    const names = (backing.kiss_tnc || [])
      .map((e) => e.interface_name)
      .filter(Boolean);
    return names.length ? `KISS-TNC: ${names.join(', ')}` : 'KISS-TNC';
  }
  return 'Unbound';
}

// aria-label per D8: "Channel N, Name, KISS-TNC loramod, backend live"
export function ariaLabel(ch) {
  const parts = [`Channel ${ch?.id ?? '?'}`];
  if (ch?.name) parts.push(ch.name);
  parts.push(summaryLabel(ch?.backing).replace(':', '').trim());
  const h = ch?.backing?.health;
  if (h === HEALTH_LIVE) parts.push('backend live');
  else if (h === HEALTH_DOWN) parts.push('backend down');
  else parts.push('backend unbound');
  return parts.join(', ');
}

// Tooltip text: prefer an explicit modem reason; fall back to the
// concatenated KISS-TNC last_error values. Empty when nothing to show.
export function tooltipText(backing) {
  if (!backing) return '';
  if (backing.modem && backing.modem.reason) return backing.modem.reason;
  const errs = (backing.kiss_tnc || [])
    .map((e) => e.last_error)
    .filter((s) => typeof s === 'string' && s.length > 0);
  return errs.join('; ');
}

// TX-capability reason wire constants. Mirror of
// dto.TxReasonNoInputDevice / dto.TxReasonNoOutputDevice on the Go
// side (pkg/webapi/dto/channel.go) so the Svelte callout text can
// match, localize, or test against named tokens without
// string-matching the backend verbatim. If either string changes on
// one side, the other MUST be updated in the same commit.
// Must match dto.TxReasonNoInputDevice.
export const TX_REASON_NO_INPUT_DEVICE = 'no input device configured';
// Must match dto.TxReasonNoOutputDevice.
export const TX_REASON_NO_OUTPUT_DEVICE = 'no output device configured';

// Fallback reason used when the server declared a channel !tx.capable
// but supplied an empty reason string (contract violation per Phase 1,
// but we still render something useful). Consumed by both the form
// callout paths and channelRefStatus.js so the two surfaces stay in
// sync.
export const TX_REASON_FALLBACK = 'not TX-capable';

// capabilityFilter predicate for ChannelListbox. Matches the shape
// `(channel) => { ok: boolean, reason: string }` documented in
// plan D3. The authoritative source is `channel.backing.tx.capable`
// -- do not recompute from `modem` / `kiss_tnc` fields directly.
// When `capable` is true the contract (Phase 1) guarantees `reason`
// is empty; we still coerce to '' defensively so callers can safely
// render it.
export function txPredicate(channel) {
  const tx = channel?.backing?.tx;
  return {
    ok: Boolean(tx?.capable),
    reason: tx?.reason ?? '',
  };
}

// Convenience boolean form of txPredicate, for list-card rendering
// that just needs "is this channel safe to transmit on right now?"
// without the reason string. Phase 3 uses this on the
// beacon/iGate/digi list surfaces.
export function isTxCapable(channel) {
  return Boolean(channel?.backing?.tx?.capable);
}
