// Tests for the deep packet inspection helpers. Pure JS, no DOM, so these
// run under `node --test` without the (absent) frontend node_modules.

import { test } from 'node:test';
import assert from 'node:assert/strict';
import { decodeRaw, hexDump, analyzeFrame } from './packetInspect.js';

// Build a 7-byte AX.25 address: callsign shifted left 1, SSID byte carries
// the SSID (bits 1-4) and the HDLC extension bit (bit 0) when `last`.
function addr(call, ssid = 0, { last = false, hbit = false } = {}) {
  const bytes = [];
  const padded = call.toUpperCase().padEnd(6, ' ');
  for (let i = 0; i < 6; i++) bytes.push(padded.charCodeAt(i) << 1);
  let ssidByte = 0x60 | (ssid << 1); // bits 5,6 reserved = 1 per spec
  if (hbit) ssidByte |= 0x80;
  if (last) ssidByte |= 0x01;
  bytes.push(ssidByte);
  return bytes;
}

function frame(dest, source, info, { control = 0x03, pid = 0xf0 } = {}) {
  const src = typeof source === 'string' ? addr(source, 0, { last: true }) : source;
  const dst = typeof dest === 'string' ? addr(dest) : dest;
  const bytes = [...dst, ...src, control, pid];
  for (const ch of info) bytes.push(typeof ch === 'number' ? ch : ch.charCodeAt(0));
  return new Uint8Array(bytes);
}

function toB64(bytes) {
  let s = '';
  for (const b of bytes) s += String.fromCharCode(b);
  return btoa(s);
}

test('decodeRaw round-trips base64 to bytes', () => {
  const bytes = new Uint8Array([0x00, 0x41, 0xff, 0x7e]);
  assert.deepEqual([...decodeRaw(toB64(bytes))], [...bytes]);
});

test('decodeRaw returns empty array for null/garbage', () => {
  assert.equal(decodeRaw(null).length, 0);
  assert.equal(decodeRaw('').length, 0);
  assert.equal(decodeRaw('!!!not base64!!!').length, 0);
});

test('hexDump formats offset, hex and ascii with non-printables as dots', () => {
  const rows = hexDump(new Uint8Array([0x48, 0x69, 0x00, 0x7f]));
  assert.equal(rows.length, 1);
  assert.equal(rows[0].offset, '0000');
  assert.equal(rows[0].ascii.trimEnd(), 'Hi..');
  assert.ok(rows[0].hex.startsWith('48 69 00 7f'));
});

test('hexDump wraps every 16 bytes', () => {
  const rows = hexDump(new Uint8Array(33));
  assert.equal(rows.length, 3);
  assert.equal(rows[1].offset, '0010');
  assert.equal(rows[2].offset, '0020');
});

test('analyzeFrame parses a clean APRS position frame with no issues', () => {
  const f = frame('APRS', 'NW5W', '!4903.50N/07201.75W-Test');
  const r = analyzeFrame(f);
  assert.equal(r.ok, true);
  assert.equal(r.dest.callsign, 'APRS');
  assert.equal(r.source.callsign, 'NW5W');
  assert.equal(r.control, 0x03);
  assert.equal(r.pid, 0xf0);
  assert.equal(r.issues.length, 0);
});

test('analyzeFrame reads SSID and digipeater path', () => {
  const f = frame(
    addr('APRS'),
    [...addr('NW5W', 7), ...addr('WIDE1', 1, { last: true, hbit: true })],
    '>status',
  );
  const r = analyzeFrame(f);
  assert.equal(r.source.callsign, 'NW5W');
  assert.equal(r.source.ssid, 7);
  assert.equal(r.digis.length, 1);
  assert.equal(r.digis[0].callsign, 'WIDE1');
  assert.equal(r.digis[0].ssid, 1);
  assert.equal(r.digis[0].hbit, true);
});

test('analyzeFrame flags too-short frames', () => {
  const r = analyzeFrame(new Uint8Array([0x01, 0x02, 0x03]));
  assert.equal(r.ok, false);
  assert.equal(r.issues[0].severity, 'error');
  assert.match(r.issues[0].text, /too short/i);
});

test('analyzeFrame flags a missing address terminator', () => {
  // Two addresses but neither has the extension bit set.
  const f = new Uint8Array([...addr('APRS'), ...addr('NW5W'), 0x03, 0xf0, 0x21]);
  const r = analyzeFrame(f);
  assert.ok(r.issues.some((i) => /no terminator/i.test(i.text)));
});

test('analyzeFrame flags invalid characters in a normal destination', () => {
  // Lowercase 'z' (0x7a) >> ... actually inject a non-callsign char directly.
  const dst = addr('APRS');
  dst[0] = '#'.charCodeAt(0) << 1; // first dest char becomes '#'
  const f = frame(dst, 'NW5W', '!nope');
  const r = analyzeFrame(f);
  assert.ok(r.issues.some((i) => i.severity === 'error' && /non-callsign/.test(i.text)));
});

test('analyzeFrame validates a well-formed Mic-E frame', () => {
  // Dest "T7SUTV" is valid Mic-E (all chars in 0-9 A-L P-Z); info starts with
  // '`' then 8+ encodable bytes (all in 0x1C-0x7F).
  const f = frame('T7SUTV', 'NW5W', '`' + 'lmnopq' + 'rst');
  const r = analyzeFrame(f);
  assert.equal(r.isMicE, true);
  assert.equal(r.issues.length, 0);
});

test('analyzeFrame accepts a low but valid Mic-E digit byte', () => {
  // Real on-air KD3DKY-7 packet: speed/course byte 0x23 (digit 7) sits
  // below the old (wrong) 0x26 floor but is a legitimate encoding.
  // Source built as an explicit addr() array — passing 'KD3DKY-7' as a
  // plain string silently encodes SSID 0, since addr() takes SSID as a
  // separate argument and doesn't parse a '-N' suffix out of the call.
  const f = frame(
    'T0TR4P',
    [...addr('KD3DKY', 7, { last: true })],
    ['`', 0x6c, 0x60, 0x3c, 0x6c, 0x23, 0x25, 0x62, 0x2f],
  );
  const r = analyzeFrame(f);
  assert.equal(r.isMicE, true);
  assert.equal(r.source.ssid, 7);
  assert.equal(r.issues.length, 0);
});

test('analyzeFrame flags a SPACE byte in the Mic-E longitude field', () => {
  const f = frame('T7SUTV', 'NW5W', ['`', 0x6c, 0x20, 0x3c, 0x6c, 0x25, 0x25, 0x62, 0x2f]);
  const r = analyzeFrame(f);
  assert.ok(r.issues.some((i) => i.severity === 'error' && /SPACE/.test(i.text)));
});

test('analyzeFrame flags a SPACE byte in the Mic-E longitude degrees byte', () => {
  // DL9DAK pre-GPS-lock shape: SPACE in the degrees byte.
  const f = frame('T7SUTV', 'NW5W', ['`', 0x20, 0x55, 0x68, 0x6c, 0x25, 0x25, 0x62, 0x2f]);
  const r = analyzeFrame(f);
  assert.ok(r.issues.some((i) => i.severity === 'error' && /SPACE/.test(i.text)));
});

test('analyzeFrame does not flag SPACE in the Mic-E longitude hundredths byte', () => {
  // GH #652: hundredths are plain hh+28, so 0x20 is the digit 04
  // (72 51.04 here), not the unknown-data sentinel.
  const f = frame('TRUW63', 'NW5W', ['`', 0x64, 0x4f, 0x20, 0x6c, 0x25, 0x25, 0x62, 0x2f]);
  const r = analyzeFrame(f);
  assert.deepEqual(r.issues, []);
});

test('analyzeFrame does not flag a DEL byte in the Mic-E longitude field', () => {
  // 0x7f is an ordinary top-of-range digit (99), never flagged — mice.go
  // no longer special-cases it against the dest's +100 offset bit either.
  const f = frame('T7SUTV', 'NW5W', ['`', 0x7f, 0x2e, 0x4f, 0x6c, 0x25, 0x25, 0x62, 0x2f]);
  const r = analyzeFrame(f);
  assert.equal(r.issues.length, 0);
});

test('analyzeFrame does not flag a DEL byte in the Mic-E minutes/hundredths bytes', () => {
  // Real DL8XI longitude bytes: degrees=DEL, minutes='(', hundredths=DEL.
  const f = frame('T7SUTV', 'NW5W', ['`', 0x7f, 0x28, 0x7f, 0x6c, 0x25, 0x25, 0x62, 0x2f]);
  const r = analyzeFrame(f);
  assert.equal(r.issues.length, 0);
});

test('analyzeFrame does not flag an aligned SPACE byte in the Mic-E speed/course field', () => {
  // In the speed/course bytes 0x20 is just digit 4 and the Go decoder
  // reads it normally (this frame decodes to 30 kt / 84 degrees). Only
  // the longitude degrees/minutes bytes treat SPACE as the "unknown data"
  // sentinel.
  for (const i of [4, 5, 6]) {
    const info = ['`', 0x6c, 0x6d, 0x6e, 0x6f, 0x70, 0x71, 0x72, 0x2f];
    info[i] = 0x20;
    const r = analyzeFrame(frame('T7SUTV', 'NW5W', info));
    assert.deepEqual(r.issues, [], `SPACE at info offset ${i}`);
  }
});

test('analyzeFrame warns on a squashed Mic-E speed/course field', () => {
  // A double space collapsed to one shifts the symbol code+table a byte
  // early: info[7] is '/', info[8] is not a table. Same shape the Go
  // decoder's accept_broken_mice realigns (to symbol '/>').
  const f = frame('T7SUTV', 'NW5W', ['`', 0x6c, 0x6d, 0x6e, 0x6c, 0x20, 0x3e, 0x2f, 0x5d]);
  const r = analyzeFrame(f);
  assert.ok(r.issues.some((i) => i.severity === 'warn' && /squashed/.test(i.text)));
  assert.ok(!r.issues.some((i) => i.severity === 'error'));
});

test('analyzeFrame accepts live Mic-E zero-speed wrap bytes', () => {
  // F4MLV-7>4R5WV3,TCPIP:`w26l `[/"=I}
  // The speed/course triplet 0x6c 0x20 0x60 encodes 0 kt / 68 degrees.
  // 0x20 is below printable-safe 0x26 but is valid Mic-E (offset +28).
  const f = new Uint8Array([
    0x68, 0xa4, 0x6a, 0xae, 0xac, 0x66, 0xe0, 0x8c,
    0x68, 0x9a, 0x98, 0xac, 0x40, 0x6e, 0xa8, 0x86,
    0xa0, 0x92, 0xa0, 0x40, 0x61, 0x03, 0xf0, 0x60,
    0x77, 0x32, 0x36, 0x6c, 0x20, 0x60, 0x5b, 0x2f,
    0x22, 0x3d, 0x49, 0x7d,
  ]);
  const r = analyzeFrame(f);
  assert.equal(r.isMicE, true);
  assert.deepEqual(r.issues, []);
});

test('analyzeFrame rejects a Mic-E offset byte below 0x1C', () => {
  const f = frame('T7SUTV', 'NW5W', [0x60, 0x77, 0x32, 0x36, 0x6c, 0x1b, 0x60, 0x5b, 0x2f]);
  const r = analyzeFrame(f);
  assert.ok(r.issues.some((i) => /outside the encodable range 0x1C-0x7F/.test(i.text)));
});

test('analyzeFrame flags malformed Mic-E destination characters', () => {
  // 'M', 'N', 'O' sit in the illegal Mic-E gap between L and P.
  const f = frame('MNOPQR', 'NW5W', '`abcdef gh');
  const r = analyzeFrame(f);
  assert.equal(r.isMicE, true);
  assert.ok(r.issues.some((i) => i.severity === 'error' && /Mic-E destination/.test(i.text)));
});

test('analyzeFrame flags a truncated Mic-E info field', () => {
  const f = frame('T7SUTV', 'NW5W', '`ab'); // only 3 info bytes, need >= 9
  const r = analyzeFrame(f);
  assert.ok(r.issues.some((i) => /Mic-E info field is/.test(i.text)));
});

test('analyzeFrame handles a frame that ends before its PID', () => {
  // dest + source (terminated) + a lone control byte, no PID.
  const f = new Uint8Array([...addr('APRS'), ...addr('NW5W', 0, { last: true }), 0x03]);
  const r = analyzeFrame(f);
  assert.equal(r.ok, true);
  assert.equal(r.control, 0x03);
  assert.equal(r.pid, null); // must stay null (drives the 0x?? render, not 0xundefined)
  assert.equal(r.isMicE, false);
  assert.ok(r.issues.some((i) => i.severity === 'warn' && /PID/.test(i.text)));
});

test('analyzeFrame flags truncated legacy Mic-E info', () => {
  const f = frame('T7SUTV', 'NW5W', [0x1c, 0x21, 0x22]);
  const r = analyzeFrame(f);
  assert.equal(r.isMicE, true);
  assert.ok(r.issues.some((i) => /needs at least 9/.test(i.text)));
});

test('analyzeFrame warns on unexpected control/PID bytes', () => {
  const f = frame('APRS', 'NW5W', '!data', { control: 0x00, pid: 0x00 });
  const r = analyzeFrame(f);
  assert.ok(r.issues.some((i) => i.severity === 'warn' && /Control byte/.test(i.text)));
  assert.ok(r.issues.some((i) => i.severity === 'warn' && /PID/.test(i.text)));
});

test('analyzeFrame recognizes all four Mic-E data identifiers', () => {
  for (const dti of [0x60, 0x27, 0x1c, 0x1d]) {
    const r = analyzeFrame(frame('T7SUTV', 'NW5W', [dti, 0x6c, 0x6d, 0x6e, 0x6f, 0x70, 0x71, 0x72, 0x2f]));
    assert.equal(r.isMicE, true);
    assert.deepEqual(r.issues, []);
  }
});
