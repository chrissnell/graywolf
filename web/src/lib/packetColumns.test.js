import test from 'node:test';
import assert from 'node:assert/strict';
import { audioLevel, displaySegments } from './packetColumns.js';

test('audioLevel: null when the packet carries no audio_level', () => {
  assert.equal(audioLevel({}), null);
  assert.equal(audioLevel({ audio_level: null }), null);
});

test('audioLevel: null when audio_level has no level_dbfs', () => {
  assert.equal(audioLevel({ audio_level: { mark: 5, space: 5 } }), null);
});

// Zone thresholds must match levelColor() in Dashboard.svelte exactly so the
// packet meter and the device meter never disagree on colour for the same
// level: red > -6, amber -20..-6, green <= -20.
test('audioLevel: zone boundaries match the device meter (red >-6, amber -20..-6, green <=-20)', () => {
  assert.equal(audioLevel({ audio_level: { level_dbfs: -3 } }).zone, 'hot');
  assert.equal(audioLevel({ audio_level: { level_dbfs: -6 } }).zone, 'warm');
  assert.equal(audioLevel({ audio_level: { level_dbfs: -10 } }).zone, 'warm');
  assert.equal(audioLevel({ audio_level: { level_dbfs: -20 } }).zone, 'good');
  assert.equal(audioLevel({ audio_level: { level_dbfs: -25 } }).zone, 'good');
});

test('audioLevel: lit maps -60..0 dBFS to 0..10 segments', () => {
  assert.equal(audioLevel({ audio_level: { level_dbfs: 0 } }).lit, 10);
  assert.equal(audioLevel({ audio_level: { level_dbfs: -6 } }).lit, 9);
  assert.equal(audioLevel({ audio_level: { level_dbfs: -30 } }).lit, 5);
  assert.equal(audioLevel({ audio_level: { level_dbfs: -60 } }).lit, 0);
  assert.equal(audioLevel({ audio_level: { level_dbfs: -90 } }).lit, 0);
});

test('audioLevel: level/mark/space rounded to integer dBFS', () => {
  const r = audioLevel({ audio_level: { level_dbfs: -4.1, mark_dbfs: -3.7, space_dbfs: -4.4 } });
  assert.equal(r.level, -4);
  assert.equal(r.mark, -4);
  assert.equal(r.space, -4);
});

test('audioLevel: mark/space fall back to level_dbfs when absent', () => {
  const r = audioLevel({ audio_level: { level_dbfs: -12 } });
  assert.equal(r.mark, -12);
  assert.equal(r.space, -12);
});

test('displaySegments: all-printable text is one run', () => {
  assert.deepEqual(displaySegments('K7XYZ>APRS:!hello'), [{ text: 'K7XYZ>APRS:!hello' }]);
});

test('displaySegments: empty / null input yields no segments', () => {
  assert.deepEqual(displaySegments(''), []);
  assert.deepEqual(displaySegments(null), []);
  assert.deepEqual(displaySegments(undefined), []);
});

// The motivating case from GH #376: a 0x7F (DEL) wedged into a beacon.
test('displaySegments: 0x7F (DEL) becomes a styled <0x7f> token', () => {
  const segs = displaySegments('K7XYZ>APRS:!47\x7f12.34N');
  assert.deepEqual(segs, [
    { text: 'K7XYZ>APRS:!47' },
    { ctrl: true, code: 0x7f, label: '<0x7f>', title: 'non-printable byte 0x7f' },
    { text: '12.34N' },
  ]);
});

test('displaySegments: C0 control bytes are flagged with 2-digit hex', () => {
  assert.deepEqual(displaySegments('a\x00b\x1fc'), [
    { text: 'a' },
    { ctrl: true, code: 0x00, label: '<0x00>', title: 'non-printable byte 0x00' },
    { text: 'b' },
    { ctrl: true, code: 0x1f, label: '<0x1f>', title: 'non-printable byte 0x1f' },
    { text: 'c' },
  ]);
});

test('displaySegments: consecutive non-printables each get their own token', () => {
  assert.deepEqual(displaySegments('\x01\x02'), [
    { ctrl: true, code: 0x01, label: '<0x01>', title: 'non-printable byte 0x01' },
    { ctrl: true, code: 0x02, label: '<0x02>', title: 'non-printable byte 0x02' },
  ]);
});

test('displaySegments: C1 controls and U+FFFD are flagged, ordinary Unicode is not', () => {
  // Accented status text and emoji stay in the printable run...
  assert.deepEqual(displaySegments('café'), [{ text: 'café' }]);
  // ...but a C1 control (U+0085) and the replacement char are surfaced; U+FFFD
  // marks a byte Go could not encode as valid UTF-8, so its tooltip says so.
  assert.deepEqual(displaySegments('xy�z'), [
    { text: 'x' },
    { ctrl: true, code: 0x85, label: '<0x85>', title: 'non-printable byte 0x85' },
    { text: 'y' },
    { ctrl: true, code: 0xfffd, label: '<0xfffd>', title: 'invalid byte (replaced with U+FFFD in transit)' },
    { text: 'z' },
  ]);
});

test('displaySegments: space and tab are treated correctly (space printable, tab not)', () => {
  assert.deepEqual(displaySegments('a b\tc'), [
    { text: 'a b' },
    { ctrl: true, code: 0x09, label: '<0x09>', title: 'non-printable byte 0x09' },
    { text: 'c' },
  ]);
});
