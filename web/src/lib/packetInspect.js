// Deep packet inspection helpers for the APRS Logs tab.
//
// Pure JS — no runes, no DOM, no imports. Turns a packet's raw AX.25 frame
// (delivered as a base64 string in Entry.raw) into a hex/ASCII dump plus a
// best-effort structural decode with error detection, the way direwolf's
// packet decoder does. Used by PacketInspector.svelte and exercised by
// packetInspect.test.js.
//
// The FCS is stripped upstream (see packetlog.Entry.Raw), so checksum
// validation is intentionally not attempted here.

// Data type identifiers that mark a Mic-E payload. Kept in sync with the
// Go decoder's dispatch (pkg/aprs/parse.go), which treats only '`' (current)
// and '\'' (old) as Mic-E so the inspector's "Mic-E" label matches what the
// rest of graywolf actually decodes.
const MICE_TYPE_BYTES = new Set([0x60, 0x27]);

// AX.25 control/PID expected for an APRS UI frame.
const AX25_UI_CONTROL = 0x03;
const AX25_PID_NO_LAYER3 = 0xf0;

/**
 * Decode a base64 frame (Entry.raw) into a Uint8Array.
 * Returns an empty array for null/empty/garbage input rather than throwing,
 * so the inspector can always render something.
 */
export function decodeRaw(b64) {
  if (!b64) return new Uint8Array(0);
  try {
    const bin = atob(b64);
    const out = new Uint8Array(bin.length);
    for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
    return out;
  } catch (_) {
    return new Uint8Array(0);
  }
}

/**
 * Build a classic hex/ASCII dump: 16 bytes per row, offset gutter, a gap
 * after the 8th byte, and a printable-ASCII sidebar (non-printables shown
 * as '.'). Returns one object per row so the view can lay it out in a grid.
 */
export function hexDump(bytes) {
  const rows = [];
  for (let off = 0; off < bytes.length; off += 16) {
    const slice = bytes.subarray(off, off + 16);
    const hex = [];
    let ascii = '';
    for (let i = 0; i < 16; i++) {
      if (i < slice.length) {
        hex.push(slice[i].toString(16).padStart(2, '0'));
        const c = slice[i];
        ascii += c >= 0x20 && c < 0x7f ? String.fromCharCode(c) : '.';
      } else {
        hex.push('  ');
        ascii += ' ';
      }
    }
    rows.push({
      offset: off.toString(16).padStart(4, '0'),
      hex: hex.slice(0, 8).join(' ') + '  ' + hex.slice(8).join(' '),
      ascii,
    });
  }
  return rows;
}

// Decode one 7-byte AX.25 address (6 shifted callsign bytes + SSID byte).
function parseAddress(bytes, off) {
  let raw = '';
  for (let i = 0; i < 6; i++) raw += String.fromCharCode(bytes[off + i] >> 1);
  const ssidByte = bytes[off + 6];
  return {
    raw, // 6 chars, space-padded, before trimming — used for char validation
    callsign: raw.replace(/ +$/, ''),
    ssid: (ssidByte >> 1) & 0x0f,
    last: (ssidByte & 0x01) === 1, // HDLC extension bit: set on final address
    hbit: (ssidByte & 0x80) !== 0, // has-been-repeated / command bit
  };
}

const STD_CALL_CHAR = /^[A-Z0-9 ]$/;
// Valid Mic-E destination characters (APRS101 ch.10): each encodes a latitude
// digit plus message/ambiguity/N-S/E-W/long-offset bits. The legal set is
// 0-9, A-L and P-Z; the gaps (':'..'@', 'M'..'O') are malformed.
const MICE_DEST_CHAR = /^[0-9A-LP-Z]$/;

/**
 * Structurally decode an AX.25/APRS frame and flag likely problems.
 *
 * Returns { ok, addresses, dest, source, digis, control, pid, infoStart,
 * isMicE, issues }. `issues` is an array of { severity: 'error'|'warn', text }.
 * `ok` is false only when the frame is too short / malformed to parse at all;
 * partial decodes still return what was recoverable alongside the issues.
 */
export function analyzeFrame(bytes) {
  const issues = [];
  const result = {
    ok: false,
    addresses: [],
    dest: null,
    source: null,
    digis: [],
    control: null,
    pid: null,
    infoStart: -1,
    isMicE: false,
    issues,
  };

  if (bytes.length < 15) {
    issues.push({
      severity: 'error',
      text: `Frame too short (${bytes.length} bytes) to hold a dest + source address and control field.`,
    });
    return result;
  }

  // Walk the address field: 7 bytes each until the extension bit is set.
  const addresses = [];
  let off = 0;
  let terminated = false;
  while (off + 7 <= bytes.length && addresses.length < 10) {
    const addr = parseAddress(bytes, off);
    addresses.push(addr);
    off += 7;
    if (addr.last) {
      terminated = true;
      break;
    }
  }
  result.addresses = addresses;
  result.dest = addresses[0] || null;
  result.source = addresses[1] || null;
  result.digis = addresses.slice(2);

  if (!terminated) {
    issues.push({
      severity: 'error',
      text: 'Address field has no terminator (HDLC extension bit never set within 10 addresses).',
    });
    return result;
  }
  if (addresses.length < 2) {
    issues.push({ severity: 'error', text: 'Frame is missing a source address.' });
    return result;
  }

  result.control = off < bytes.length ? bytes[off] : null;
  result.pid = off + 1 < bytes.length ? bytes[off + 1] : null;
  result.infoStart = off + 2;
  result.ok = true;

  // Mic-E lives in the destination address, so detect it from the info type
  // byte before validating the destination characters.
  const info = bytes.subarray(result.infoStart);
  result.isMicE = info.length > 0 && MICE_TYPE_BYTES.has(info[0]);

  validateAddresses(result, issues);
  validateControl(result, issues);
  if (result.isMicE) validateMicE(result, info, issues);

  return result;
}

function validateAddresses(result, issues) {
  const dest = result.dest;
  if (dest) {
    if (result.isMicE) {
      const bad = [...dest.raw].filter((c) => !MICE_DEST_CHAR.test(c));
      if (bad.length) {
        issues.push({
          severity: 'error',
          text: `Mic-E destination "${dest.raw}" has invalid character(s) ${quoteChars(bad)}; Mic-E latitude digits must be 0-9, A-L or P-Z.`,
        });
      }
    } else {
      const bad = [...dest.raw].filter((c) => !STD_CALL_CHAR.test(c));
      if (bad.length) {
        issues.push({
          severity: 'error',
          text: `Destination address "${dest.callsign}" contains non-callsign character(s) ${quoteChars(bad)}; expected A-Z, 0-9 or space.`,
        });
      }
    }
  }

  const src = result.source;
  if (src) {
    const bad = [...src.raw].filter((c) => !STD_CALL_CHAR.test(c));
    if (bad.length) {
      issues.push({
        severity: 'error',
        text: `Source address "${src.callsign}" contains non-callsign character(s) ${quoteChars(bad)}; expected A-Z, 0-9 or space.`,
      });
    }
    if (src.callsign === '') {
      issues.push({ severity: 'error', text: 'Source callsign is empty.' });
    }
  }
}

function validateControl(result, issues) {
  if (result.control !== AX25_UI_CONTROL) {
    issues.push({
      severity: 'warn',
      text: `Control byte is 0x${hexByte(result.control)}; APRS expects a UI frame (0x03).`,
    });
  }
  if (result.pid !== AX25_PID_NO_LAYER3) {
    issues.push({
      severity: 'warn',
      text: `PID is 0x${hexByte(result.pid)}; APRS expects no layer-3 protocol (0xF0).`,
    });
  }
}

// Mic-E info field: type byte + 3 longitude + 3 speed/course + symbol +
// symbol-table = 9 bytes minimum (APRS101 ch.10). The longitude/speed bytes
// are offset-encoded printable ASCII (0x26-0x7f).
function validateMicE(result, info, issues) {
  if (info.length < 9) {
    issues.push({
      severity: 'error',
      text: `Mic-E info field is ${info.length} bytes; needs at least 9 (longitude + speed/course + symbol).`,
    });
    return;
  }
  for (let i = 1; i <= 6; i++) {
    const b = info[i];
    if (b < 0x26 || b > 0x7f) {
      issues.push({
        severity: 'error',
        text: `Mic-E longitude/speed byte at info offset ${i} is 0x${hexByte(b)}, outside the encodable range 0x26-0x7F.`,
      });
      break;
    }
  }
}

function quoteChars(chars) {
  return chars.map((c) => `'${c === ' ' ? '\\x20' : c}'`).join(', ');
}

function hexByte(b) {
  return b == null ? '??' : b.toString(16).padStart(2, '0').toUpperCase();
}
