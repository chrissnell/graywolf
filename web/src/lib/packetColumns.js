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

/**
 * Per-packet received audio level for the signal meter, expressed in dBFS so it
 * shares the real-time device meter's unit (Dashboard / Audio Devices) — a
 * −25 dBFS signal reads ≈ −25 in both places. Returns null when the packet
 * carries no modem audio level (TX, APRS-IS, hardware KISS-TNC, or a frame that
 * failed to decode before levels were attached), so the cell renders a dash.
 *
 * `level` is the overall reading in integer dBFS; `mark`/`space` expose the
 * per-tone split in dBFS (a large spread is audio "twist"); `lit` is how many of
 * the 10 meter segments to fill (−60…0 dBFS mapped to 0…10); `zone` colours the
 * meter using the exact same thresholds as the device meter's `levelColor`
 * (web/src/routes/Dashboard.svelte), so identical audio reads the same colour
 * in both places:
 *   good (≤ −20 dBFS)  nominal received level — green
 *   warm (−20…−6)      hotter than nominal — amber
 *   hot  (> −6)        clipping risk — red
 */
export function audioLevel(pkt) {
  const a = pkt.audio_level;
  if (!a || a.level_dbfs == null) return null;
  const level = Math.round(a.level_dbfs);
  const mark = Math.round(a.mark_dbfs ?? a.level_dbfs);
  const space = Math.round(a.space_dbfs ?? a.level_dbfs);
  const clamped = Math.max(-60, Math.min(0, a.level_dbfs));
  const lit = Math.max(0, Math.min(10, Math.round(((clamped + 60) / 60) * 10)));
  let zone = 'good';
  if (a.level_dbfs > -6) zone = 'hot';
  else if (a.level_dbfs > -20) zone = 'warm';
  return { level, mark, space, lit, zone };
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
 * Split a packet display string into printable runs and individual
 * non-printable bytes for the log line. APRS payloads should be plain
 * printable ASCII, but a malformed packet can carry control bytes — most
 * famously a 0x7F (DEL) wedged into a position report (GH #376) that renders
 * invisibly yet silently breaks map plotting. We surface each one as a styled
 * `<0x7f>` token (aprs.fi's convention) so the caller can colour it distinctly
 * from text that merely happens to read "<0x7f>".
 *
 * Returns an array of segments: `{ text }` for a printable run, or
 * `{ ctrl: true, code, label, title }` for one non-printable byte. A code point
 * is treated as non-printable when it is a C0 control (< 0x20), DEL (0x7F), a C1
 * control (0x80–0x9F), or U+FFFD. In practice the two forms that reach the
 * browser are DEL/C0 (valid single-byte UTF-8, passed through Go's json.Marshal
 * untouched) and U+FFFD — the replacement char Go substitutes for any byte that
 * is not valid UTF-8, which is how most corrupted high bytes actually arrive.
 * The literal C1 range (0x80–0x9F) is only reachable if a payload carried those
 * code points as valid 2-byte UTF-8; it is kept defensively rather than because
 * the current backend emits it. Ordinary higher Unicode — accented status text
 * and the like — is left in the printable run untouched.
 */
export function displaySegments(str) {
  const out = [];
  let run = '';
  const flush = () => {
    if (run) {
      out.push({ text: run });
      run = '';
    }
  };
  for (const ch of str || '') {
    const cp = ch.codePointAt(0);
    const ctrl = cp < 0x20 || cp === 0x7f || (cp >= 0x80 && cp <= 0x9f) || cp === 0xfffd;
    if (ctrl) {
      flush();
      const title =
        cp === 0xfffd
          ? 'invalid byte (replaced with U+FFFD in transit)'
          : `non-printable byte 0x${cp.toString(16).padStart(2, '0')}`;
      out.push({ ctrl: true, code: cp, label: `<0x${cp.toString(16).padStart(2, '0')}>`, title });
    } else {
      run += ch;
    }
  }
  flush();
  return out;
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
