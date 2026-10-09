import { strict as assert } from 'node:assert';
import { describe, it } from 'node:test';

import { SOUND_PRESETS, resolvePreset, playPreset } from './soundPresets.js';

describe('SOUND_PRESETS', () => {
  it('has unique ids', () => {
    const ids = SOUND_PRESETS.map((p) => p.id);
    assert.equal(new Set(ids).size, ids.length);
  });

  it('every preset has a non-empty label', () => {
    for (const p of SOUND_PRESETS) {
      assert.ok(p.label && p.label.length > 0, `preset ${p.id} missing a label`);
    }
  });

  it('every preset has exactly one of url (shipped file) or tones (synthesized)', () => {
    for (const p of SOUND_PRESETS) {
      const hasUrl = !!p.url;
      const hasTones = Array.isArray(p.tones) && p.tones.length > 0;
      assert.notEqual(hasUrl, hasTones, `preset ${p.id} must have exactly one of url/tones`);
    }
  });

  it('the shipped aprs-message/aprs-bulletin defaults point under /sounds/', () => {
    const message = SOUND_PRESETS.find((p) => p.id === 'aprs-message');
    const bulletin = SOUND_PRESETS.find((p) => p.id === 'aprs-bulletin');
    assert.ok(message, 'aprs-message preset must exist');
    assert.ok(bulletin, 'aprs-bulletin preset must exist');
    assert.match(message.url, /^\/sounds\/.+\.wav$/);
    assert.match(bulletin.url, /^\/sounds\/.+\.wav$/);
  });

  it('the shipped emergency-mp3 default (station emergency) points under /sounds/', () => {
    const emergency = SOUND_PRESETS.find((p) => p.id === 'emergency-mp3');
    assert.ok(emergency, 'emergency-mp3 preset must exist');
    assert.match(emergency.url, /^\/sounds\/.+\.mp3$/);
  });

  it('tone presets carry a positive freq and dur on every tone', () => {
    for (const p of SOUND_PRESETS) {
      if (!p.tones) continue;
      for (const tone of p.tones) {
        assert.ok(tone.freq > 0, `preset ${p.id} has a non-positive tone freq`);
        assert.ok(tone.dur > 0, `preset ${p.id} has a non-positive tone dur`);
      }
    }
  });
});

describe('resolvePreset', () => {
  it('resolves a known id to its preset entry', () => {
    assert.equal(resolvePreset('chime').id, 'chime');
  });

  it('falls back to the first preset for an unknown id', () => {
    assert.equal(resolvePreset('not-a-real-id'), SOUND_PRESETS[0]);
  });
});

describe('playPreset', () => {
  it('does not throw outside a browser (no window/Audio/AudioContext available)', () => {
    // node --test runs with no `window`; playPreset must no-op rather
    // than throw a ReferenceError, so the store logic that calls it can
    // be exercised in tests without a DOM.
    assert.doesNotThrow(() => playPreset('aprs-message'));
    assert.doesNotThrow(() => playPreset('chime'));
    assert.doesNotThrow(() => playPreset('not-a-real-id'));
  });
});
