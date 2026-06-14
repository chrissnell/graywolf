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
  // '`' then 8+ encodable bytes (all in 0x26-0x7F).
  const f = frame('T7SUTV', 'NW5W', '`' + 'lmnopq' + 'rst');
  const r = analyzeFrame(f);
  assert.equal(r.isMicE, true);
  assert.equal(r.issues.length, 0);
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

test('analyzeFrame does not treat legacy 0x1c/0x1d info bytes as Mic-E', () => {
  // Matches the Go decoder, which only dispatches '`' and '\'' to Mic-E.
  const f = frame('T7SUTV', 'NW5W', [0x1c, 0x21, 0x22]);
  const r = analyzeFrame(f);
  assert.equal(r.isMicE, false);
});

test('analyzeFrame warns on unexpected control/PID bytes', () => {
  const f = frame('APRS', 'NW5W', '!data', { control: 0x00, pid: 0x00 });
  const r = analyzeFrame(f);
  assert.ok(r.issues.some((i) => i.severity === 'warn' && /Control byte/.test(i.text)));
  assert.ok(r.issues.some((i) => i.severity === 'warn' && /PID/.test(i.text)));
});
