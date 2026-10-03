import { strict as assert } from 'node:assert';
import { describe, it } from 'node:test';

import { SOUND_PRESETS } from '../soundPresets.js';
import {
  MAX_SOUND_BYTES,
  isValidPresetId,
  parsePresetId,
  parseEnabledFlag,
  validateUploadFile,
  fallbackPresetId,
} from './notification-sound-core.js';

describe('isValidPresetId', () => {
  it('accepts every id actually in SOUND_PRESETS', () => {
    for (const p of SOUND_PRESETS) {
      assert.equal(isValidPresetId(p.id), true);
    }
  });

  it('rejects "custom" (it is a distinct selection, not a preset)', () => {
    assert.equal(isValidPresetId('custom'), false);
  });

  it('rejects unknown/garbage ids', () => {
    assert.equal(isValidPresetId('nope'), false);
    assert.equal(isValidPresetId(''), false);
    assert.equal(isValidPresetId(undefined), false);
  });
});

describe('parsePresetId', () => {
  it('passes through "custom"', () => {
    assert.equal(parsePresetId('custom', 'aprs-message'), 'custom');
  });

  it('passes through any known preset id', () => {
    assert.equal(parsePresetId('chime', 'aprs-message'), 'chime');
  });

  it('falls back to the default for null/garbage/unknown ids', () => {
    assert.equal(parsePresetId(null, 'aprs-message'), 'aprs-message');
    assert.equal(parsePresetId(undefined, 'aprs-bulletin'), 'aprs-bulletin');
    assert.equal(parsePresetId('not-a-real-preset', 'ping'), 'ping');
  });
});

describe('parseEnabledFlag', () => {
  it('defaults true when never stored — sound is on by default', () => {
    assert.equal(parseEnabledFlag(null), true);
    assert.equal(parseEnabledFlag(undefined), true);
  });

  it('reads back "1"/"0" explicitly', () => {
    assert.equal(parseEnabledFlag('1'), true);
    assert.equal(parseEnabledFlag('0'), false);
  });
});

describe('validateUploadFile', () => {
  it('rejects a missing file', () => {
    assert.match(validateUploadFile(null), /no file/i);
    assert.match(validateUploadFile(undefined), /no file/i);
  });

  it('rejects a non-audio MIME type', () => {
    const err = validateUploadFile({ type: 'image/png', size: 100 });
    assert.match(err, /audio file/i);
  });

  it('accepts a file with no reported type (some browsers omit it)', () => {
    assert.equal(validateUploadFile({ type: '', size: 100 }), null);
  });

  it('rejects a file over the byte cap', () => {
    const err = validateUploadFile({ type: 'audio/wav', size: MAX_SOUND_BYTES + 1 });
    assert.match(err, /too large/i);
  });

  it('accepts a file exactly at the byte cap', () => {
    assert.equal(validateUploadFile({ type: 'audio/wav', size: MAX_SOUND_BYTES }), null);
  });

  it('accepts a valid small audio file', () => {
    assert.equal(validateUploadFile({ type: 'audio/mpeg', size: 12345 }), null);
  });

  it('honors a custom max for parametric tests', () => {
    assert.match(validateUploadFile({ type: 'audio/wav', size: 500 }, 100), /too large/i);
    assert.equal(validateUploadFile({ type: 'audio/wav', size: 50 }, 100), null);
  });
});

describe('fallbackPresetId', () => {
  it('resolves "custom" to the kind default (custom sound unavailable)', () => {
    assert.equal(fallbackPresetId('custom', 'aprs-message'), 'aprs-message');
    assert.equal(fallbackPresetId('custom', 'aprs-bulletin'), 'aprs-bulletin');
  });

  it('passes through a valid non-custom preset unchanged', () => {
    assert.equal(fallbackPresetId('chime', 'aprs-message'), 'chime');
  });

  it('falls back to the default for a corrupted/unknown non-custom value', () => {
    assert.equal(fallbackPresetId('not-a-real-preset', 'ping'), 'ping');
  });
});
