// Presentation helpers for the `backing` object returned by
// /api/channels. Every picker, card, and save-form warning routes
// through these so the glyph, label, and aria-text are consistent
// across the app.
//
// Design decisions: D7 (computed backing object), D8 (3 distinct
// glyphs + text), D17 (unbound-channel warning copy).

// Unicode glyphs chosen for shape distinctness (WCAG 1.4.1 — not
// colour alone). Filled circle / hollow circle / em-dash.
export const GLYPH_LIVE = '\u25CF'; //  ●
export const GLYPH_DOWN = '\u25CB'; //  ○
export const GLYPH_UNBOUND = '\u2014'; //  —

export const HEALTH_LIVE = 'live';
export const HEALTH_DOWN = 'down';
export const HEALTH_UNBOUND = 'unbound';

export const SUMMARY_MODEM = 'modem';
export const SUMMARY_KISS_TNC = 'kiss-tnc';
export const SUMMARY_UNBOUND = 'unbound';

// Map each health value to its glyph and short user-facing text. The
// text always renders alongside the glyph (D8) — glyph-only would
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

// Unbound-channel warning copy (D17). Non-blocking — forms render
// this above the submit button when summary === 'unbound'.
export function unboundWarning(ch) {
  const id = ch?.id ?? '?';
  return (
    `\u26A0 Channel ${id} has no backend. This beacon will be ` +
    `accepted but will not transmit until a modem or KISS-TNC is ` +
    `attached.`
  );
}
