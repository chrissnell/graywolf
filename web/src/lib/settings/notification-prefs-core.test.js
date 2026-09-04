import { strict as assert } from 'node:assert';
import { describe, it } from 'node:test';

import {
  VALID_MODES,
  parseMode,
  parseEnabledFlag,
  resolveModeAfterPermission,
  STATION_NEW_THRESHOLDS,
  DEFAULT_STATION_NEW_THRESHOLD_SECS,
  MAX_CUSTOM_THRESHOLD_SECS,
  parseStationNewThresholdSecs,
  stationNewThresholdMs,
  isPresetThresholdSecs,
  secsToCustomInput,
  customInputToSecs,
} from './notification-prefs-core.js';

describe('parseMode', () => {
  it('passes through valid modes', () => {
    for (const m of VALID_MODES) {
      if (m === 'toast') continue;
      assert.equal(parseMode(m), m);
    }
  });

  it('defaults to toast for null/undefined/garbage', () => {
    assert.equal(parseMode(null), 'toast');
    assert.equal(parseMode(undefined), 'toast');
    assert.equal(parseMode('bogus'), 'toast');
    assert.equal(parseMode(''), 'toast');
  });

  it('explicit toast round-trips', () => {
    assert.equal(parseMode('toast'), 'toast');
  });
});

describe('parseEnabledFlag', () => {
  it('defaults true when never stored (null/undefined) — on-by-default', () => {
    assert.equal(parseEnabledFlag(null), true);
    assert.equal(parseEnabledFlag(undefined), true);
  });

  it('reads back an explicit "1" as true and anything else as false', () => {
    assert.equal(parseEnabledFlag('1'), true);
    assert.equal(parseEnabledFlag('0'), false);
    assert.equal(parseEnabledFlag('garbage'), false);
  });

  it('honors an explicit default when never stored (e.g. stationNewEnabled defaults off)', () => {
    assert.equal(parseEnabledFlag(null, false), false);
    assert.equal(parseEnabledFlag(undefined, false), false);
  });

  it('an explicit default does not override a stored value', () => {
    assert.equal(parseEnabledFlag('1', false), true);
    assert.equal(parseEnabledFlag('0', true), false);
  });
});

describe('parseStationNewThresholdSecs', () => {
  it('defaults to 2 hours when never stored', () => {
    assert.equal(parseStationNewThresholdSecs(null), DEFAULT_STATION_NEW_THRESHOLD_SECS);
    assert.equal(parseStationNewThresholdSecs(undefined), 7200);
  });

  it('accepts every known threshold value, including the 0 ("Never") sentinel', () => {
    for (const opt of STATION_NEW_THRESHOLDS) {
      assert.equal(parseStationNewThresholdSecs(String(opt.value)), opt.value);
    }
  });

  it('falls back to the default for garbage or negative values', () => {
    assert.equal(parseStationNewThresholdSecs('garbage'), DEFAULT_STATION_NEW_THRESHOLD_SECS);
    assert.equal(parseStationNewThresholdSecs('-5'), DEFAULT_STATION_NEW_THRESHOLD_SECS);
  });

  it('accepts a non-preset value as a valid custom threshold', () => {
    assert.equal(parseStationNewThresholdSecs('999'), 999);
    assert.equal(parseStationNewThresholdSecs('18000'), 18000); // 5 hours, typed in
  });

  it('clamps a value above MAX_CUSTOM_THRESHOLD_SECS', () => {
    assert.equal(parseStationNewThresholdSecs(String(MAX_CUSTOM_THRESHOLD_SECS + 1000)), MAX_CUSTOM_THRESHOLD_SECS);
  });
});

describe('stationNewThresholdMs', () => {
  it('converts seconds to milliseconds', () => {
    assert.equal(stationNewThresholdMs(7200), 7_200_000);
  });

  it('maps the 0 ("Never") sentinel to Infinity', () => {
    assert.equal(stationNewThresholdMs(0), Infinity);
  });
});

describe('isPresetThresholdSecs', () => {
  it('true for every STATION_NEW_THRESHOLDS value', () => {
    for (const opt of STATION_NEW_THRESHOLDS) {
      assert.equal(isPresetThresholdSecs(opt.value), true);
    }
  });

  it('false for a custom (non-preset) value', () => {
    assert.equal(isPresetThresholdSecs(18000), false);
  });
});

describe('secsToCustomInput', () => {
  it('renders whole weeks when evenly divisible', () => {
    assert.deepEqual(secsToCustomInput(604800), { count: 1, unit: 'weeks' });
    assert.deepEqual(secsToCustomInput(1209600), { count: 2, unit: 'weeks' });
  });

  it('renders hours otherwise, rounding to the nearest whole hour', () => {
    assert.deepEqual(secsToCustomInput(18000), { count: 5, unit: 'hours' });
    assert.deepEqual(secsToCustomInput(5400), { count: 2, unit: 'hours' }); // 90 min rounds to 2h
  });

  it('never renders a zero count', () => {
    assert.deepEqual(secsToCustomInput(0), { count: 1, unit: 'hours' });
  });
});

describe('customInputToSecs', () => {
  it('converts hours and weeks to seconds', () => {
    assert.equal(customInputToSecs(5, 'hours'), 18000);
    assert.equal(customInputToSecs(2, 'weeks'), 1209600);
  });

  it('floors non-integer input and treats non-numeric/negative as 0', () => {
    assert.equal(customInputToSecs(2.9, 'hours'), 2 * 3600);
    assert.equal(customInputToSecs('bogus', 'hours'), 0);
    assert.equal(customInputToSecs(-5, 'hours'), 0);
  });

  it('clamps to MAX_CUSTOM_THRESHOLD_SECS', () => {
    assert.equal(customInputToSecs(9999, 'weeks'), MAX_CUSTOM_THRESHOLD_SECS);
  });
});

describe('resolveModeAfterPermission', () => {
  it('toast never needs permission and passes straight through', () => {
    assert.equal(resolveModeAfterPermission('toast', 'granted'), 'toast');
    assert.equal(resolveModeAfterPermission('toast', 'denied'), 'toast');
    assert.equal(resolveModeAfterPermission('toast', 'default'), 'toast');
  });

  it('os/both are kept when permission is granted', () => {
    assert.equal(resolveModeAfterPermission('os', 'granted'), 'os');
    assert.equal(resolveModeAfterPermission('both', 'granted'), 'both');
  });

  it('os/both fall back to toast on denial so the operator never lands in a dead mode', () => {
    assert.equal(resolveModeAfterPermission('os', 'denied'), 'toast');
    assert.equal(resolveModeAfterPermission('both', 'denied'), 'toast');
  });

  it('os/both fall back to toast on any non-granted permission (e.g. still "default")', () => {
    assert.equal(resolveModeAfterPermission('os', 'default'), 'toast');
  });

  it('an unrecognized requested mode falls back to toast', () => {
    assert.equal(resolveModeAfterPermission('bogus', 'granted'), 'toast');
  });
});
