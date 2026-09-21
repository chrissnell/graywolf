import { test } from 'node:test';
import assert from 'node:assert/strict';
import { NAV_PROVIDERS, isValidProvider, normalizeProvider, detectDefaultProvider } from './nav-provider-core.js';

test('NAV_PROVIDERS lists Organic Maps first, then Google Maps, then Apple Maps', () => {
  assert.deepEqual(NAV_PROVIDERS.map((p) => p.value), ['organic', 'google', 'apple']);
});

test('isValidProvider accepts the three known values and rejects anything else', () => {
  assert.equal(isValidProvider('organic'), true);
  assert.equal(isValidProvider('google'), true);
  assert.equal(isValidProvider('apple'), true);
  assert.equal(isValidProvider('bing'), false);
  assert.equal(isValidProvider(null), false);
  assert.equal(isValidProvider(undefined), false);
});

test('normalizeProvider passes through valid values and nulls out invalid/missing ones', () => {
  assert.equal(normalizeProvider('apple'), 'apple');
  assert.equal(normalizeProvider('bogus'), null);
  assert.equal(normalizeProvider(null), null);
  assert.equal(normalizeProvider(undefined), null);
});

test('detectDefaultProvider: Android always defaults to Google Maps', () => {
  assert.equal(detectDefaultProvider({ isAndroid: true, userAgent: 'Mac OS X', platform: 'MacIntel' }), 'google');
});

test('detectDefaultProvider: Mac userAgent/platform defaults to Apple Maps', () => {
  assert.equal(
    detectDefaultProvider({ platform: 'MacIntel', userAgent: 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)' }),
    'apple',
  );
});

test('detectDefaultProvider: iPhone/iPad userAgent defaults to Apple Maps', () => {
  assert.equal(
    detectDefaultProvider({ platform: 'iPhone', userAgent: 'Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X)' }),
    'apple',
  );
});

test('detectDefaultProvider: Windows defaults to Google Maps', () => {
  assert.equal(
    detectDefaultProvider({ platform: 'Win32', userAgent: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64)' }),
    'google',
  );
});

test('detectDefaultProvider: unknown/Linux desktop defaults to Google Maps', () => {
  assert.equal(
    detectDefaultProvider({ platform: 'Linux x86_64', userAgent: 'Mozilla/5.0 (X11; Linux x86_64)' }),
    'google',
  );
});

test('detectDefaultProvider: no input at all defaults to Google Maps', () => {
  assert.equal(detectDefaultProvider(), 'google');
});
