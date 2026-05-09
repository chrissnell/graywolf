// Presentation helpers for the `ptt` object returned by /api/channels.
// Mirror of channelBacking.js -- the Channels page renders a PTT
// indicator block parallel to the BACKING block (issue #112) and we
// route every label through here so the wording stays consistent if
// new methods are added to dto.ChannelPtt.

// Wire-level enum values mirrored from pkg/configstore/models.go
// (PttConfig.Method) and dto.PttMethodNone. If a new method is added
// on the Go side, add it here too -- methodLabel falls back to the
// raw string so an out-of-date UI degrades to "method=foo" rather
// than blanking the row.
export const METHOD_NONE = 'none';

const METHOD_LABELS = {
  none: 'None',
  serial_rts: 'Serial RTS',
  serial_dtr: 'Serial DTR',
  gpio: 'GPIO',
  cm108: 'CM108',
  rigctld: 'rigctld',
};

export function methodLabel(method) {
  if (!method) return 'None';
  return METHOD_LABELS[method] ?? method;
}

// summaryLine renders the indicator's right-hand text:
//   "CM108 · pin 3 · /dev/hidraw0"   when configured with a detail
//   "Serial RTS"                      method-only fallback
//   "None"                            method=none
//   "Not configured"                  ptt object missing entirely
export function summaryLine(ptt) {
  if (!ptt) return 'Not configured';
  const label = methodLabel(ptt.method);
  if (!ptt.configured) return label;
  if (ptt.detail) return `${label} · ${ptt.detail}`;
  return label;
}

// State string drives the glyph colour. Three states match BACKING's
// live/down/unbound trio so the visual vocabulary on the card stays
// consistent: configured method ~= live, explicit "none" ~= down,
// missing row ~= unbound.
export function pttState(ptt) {
  if (!ptt) return 'unbound';
  return ptt.configured ? 'live' : 'down';
}

export function ariaLabel(ptt) {
  if (!ptt) return 'PTT not configured';
  if (!ptt.configured) return 'PTT method none';
  const detail = ptt.detail ? `, ${ptt.detail}` : '';
  return `PTT ${methodLabel(ptt.method)}${detail}`;
}
