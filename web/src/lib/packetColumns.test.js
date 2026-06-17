import test from 'node:test';
import assert from 'node:assert/strict';
import { audioLevel } from './packetColumns.js';

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
