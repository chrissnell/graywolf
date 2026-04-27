// Shared helpers for rendering APRS packets in the LogViewer.
// Pure JS — no runes, no DOM. Imported by PacketLogViewer.svelte and
// (potentially) other consumers that need to format packet fields.

/**
 * Extract source and destination callsigns from a packet.
 * Prefers the decoded form; falls back to parsing the raw TNC2 display string.
 */
export function parseDisplay(pkt) {
  const d = pkt.decoded;
  if (d?.source) return { src: d.source, dst: d.dest || '' };
  const s = pkt.display || '';
  const gt = s.indexOf('>');
  if (gt < 0) return { src: '', dst: '' };
  const src = s.substring(0, gt);
  const rest = s.substring(gt + 1);
  const end = rest.search(/[,:]/);
  const dst = end >= 0 ? rest.substring(0, end) : rest;
  return { src, dst };
}

/**
 * Categorize a packet's origin (digipeater / beacon / iGate variants).
 * Returns null if the packet has no origin tag worth showing.
 */
export function originTag(pkt) {
  const src = pkt.source || '';
  const notes = pkt.notes || '';
  switch (src) {
    case 'digipeater': return { label: 'Digipeater', cls: 'digi' };
    case 'beacon':     return { label: 'Beacon',     cls: 'bcn' };
    case 'igate':
      if (notes === 'is2rf') return { label: 'iGate IS\u2192RF', cls: 'igate-is2rf' };
      if (notes === 'rf2is') return { label: 'iGate RF\u2192IS', cls: 'igate-rf2is' };
      return { label: 'iGate IS RX', cls: 'igate' };
    case 'igate-is': return { label: 'iGate IS RX', cls: 'igate' };
    default: return null;
  }
}

/** Format a packet's device info as "Vendor Model" (or just one if only one is known). */
export function deviceLabel(pkt) {
  const dev = pkt.device;
  if (!dev) return '';
  if (dev.vendor && dev.model) return `${dev.vendor} ${dev.model}`;
  return dev.model || dev.vendor || '';
}

/** Format a timestamp as "M/D HH:MM:SS" in local time. */
export function formatTime(ts) {
  const d = new Date(ts);
  const mo = d.getMonth() + 1;
  const day = d.getDate();
  const h = d.getHours().toString().padStart(2, '0');
  const m = d.getMinutes().toString().padStart(2, '0');
  const s = d.getSeconds().toString().padStart(2, '0');
  return `${mo}/${day} ${h}:${m}:${s}`;
}

/**
 * Map an APRS packet's direction to a Chonky LogEntry `level`. The level
 * drives Chonky's color class on each row/card:
 *   RX → 'info'  (log-ok,  accent / greenish)
 *   TX → 'warn'  (log-warn, yellow/amber)
 *   IS → 'debug' (log-dim,  muted gray)
 * Anything else falls back to 'info'.
 */
export function directionToLevel(direction) {
  switch ((direction || '').toUpperCase()) {
    case 'RX': return 'info';
    case 'TX': return 'warn';
    case 'IS': return 'debug';
    default:   return 'info';
  }
}

/**
 * Project a raw packet into a Chonky LogEntry. Adds the `level` field
 * (so Chonky's level→class mapping picks up direction colour) without
 * mutating the original packet. The original direction is preserved on
 * the entry so the Direction badge snippet can still render its label.
 */
export function packetToEntry(pkt) {
  return { ...pkt, level: directionToLevel(pkt.direction) };
}
