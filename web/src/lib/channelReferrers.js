// Presentation helpers for the Phase 5 channel-delete two-step flow.
//
// The API returns a flat list of { type, id, name } items; the UI needs
// them grouped + labeled in user-facing language. This module is the
// single source of truth for those labels — the Channels page imports
// it; any future page that surfaces referrer info should reuse it.

export const REFERRER_TYPE = Object.freeze({
  BEACON: 'beacon',
  DIGI_FROM: 'digipeater_rule_from',
  DIGI_TO: 'digipeater_rule_to',
  KISS: 'kiss_interface',
  IGATE_RF: 'igate_config_rf',
  IGATE_TX: 'igate_config_tx',
  IGATE_FILTER: 'igate_rf_filter',
  TX_TIMING: 'tx_timing',
});

// Human-readable singular / plural labels per referrer type. Separated
// from the actionable hint so the group heading stays short.
const LABELS = {
  [REFERRER_TYPE.BEACON]: { singular: 'Beacon', plural: 'Beacons' },
  [REFERRER_TYPE.DIGI_FROM]: { singular: 'Digipeater rule', plural: 'Digipeater rules' },
  [REFERRER_TYPE.DIGI_TO]: { singular: 'Digipeater rule (destination)', plural: 'Digipeater rules (destination)' },
  [REFERRER_TYPE.KISS]: { singular: 'KISS interface', plural: 'KISS interfaces' },
  [REFERRER_TYPE.IGATE_RF]: { singular: 'iGate RF channel', plural: 'iGate RF channel' },
  [REFERRER_TYPE.IGATE_TX]: { singular: 'iGate TX channel', plural: 'iGate TX channel' },
  [REFERRER_TYPE.IGATE_FILTER]: { singular: 'iGate RF filter', plural: 'iGate RF filters' },
  [REFERRER_TYPE.TX_TIMING]: { singular: 'TX timing record', plural: 'TX timing records' },
};

// Post-cascade action hint per referrer type. Surfaces in the
// operator-facing dialog so they understand what cascade will do to
// each row (deleted vs nulled vs "cleared").
const ACTIONS = {
  [REFERRER_TYPE.BEACON]: 'will be deleted',
  [REFERRER_TYPE.DIGI_FROM]: 'will be deleted',
  [REFERRER_TYPE.DIGI_TO]: 'will be deleted',
  [REFERRER_TYPE.KISS]: 'will be disabled, needs reconfig',
  [REFERRER_TYPE.IGATE_RF]: 'assignment will be cleared',
  [REFERRER_TYPE.IGATE_TX]: 'assignment will be cleared',
  [REFERRER_TYPE.IGATE_FILTER]: 'will be deleted',
  [REFERRER_TYPE.TX_TIMING]: 'will be deleted',
};

// Stable ordering for display so the dialog reads the same on every
// render. Rules / beacons first because they're most visible; iGate
// config edits are less surprising (they clear a scalar, not a row).
const ORDER = [
  REFERRER_TYPE.BEACON,
  REFERRER_TYPE.DIGI_FROM,
  REFERRER_TYPE.DIGI_TO,
  REFERRER_TYPE.KISS,
  REFERRER_TYPE.IGATE_FILTER,
  REFERRER_TYPE.TX_TIMING,
  REFERRER_TYPE.IGATE_RF,
  REFERRER_TYPE.IGATE_TX,
];

// groupReferrers takes the flat API list and returns an array of
// { type, label, action, items } groups in display order. Empty
// groups are omitted. Each item still carries { id, name } so the
// caller can render them as list entries.
export function groupReferrers(referrers) {
  if (!Array.isArray(referrers) || referrers.length === 0) return [];
  const byType = {};
  for (const r of referrers) {
    if (!r || !r.type) continue;
    (byType[r.type] ||= []).push(r);
  }
  const out = [];
  for (const type of ORDER) {
    const items = byType[type];
    if (!items || items.length === 0) continue;
    const labelSet = LABELS[type] || { singular: type, plural: type };
    out.push({
      type,
      label: items.length === 1 ? labelSet.singular : labelSet.plural,
      action: ACTIONS[type] || '',
      items,
    });
  }
  // Surface any unknown types so a future backend addition doesn't
  // silently disappear from the UI. They go at the end.
  for (const [type, items] of Object.entries(byType)) {
    if (ORDER.includes(type)) continue;
    out.push({ type, label: type, action: '', items });
  }
  return out;
}

// totalReferrers returns the total count across all groups — the
// red-button copy says "Delete channel and N references".
export function totalReferrers(referrers) {
  if (!Array.isArray(referrers)) return 0;
  return referrers.length;
}
