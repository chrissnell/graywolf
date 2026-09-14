import { test } from 'node:test';
import assert from 'node:assert/strict';
import { organicMapsLinks, googleMapsUrl, appleMapsLinks } from './nav-links.js';

test('organicMapsLinks builds bare-coordinate scheme + fallback with no name', () => {
  assert.deepEqual(organicMapsLinks(48.858093, 2.294694), {
    scheme: 'om://48.858093,2.294694',
    fallback: 'https://omaps.app/48.858093,2.294694',
  });
});

test('organicMapsLinks appends an underscore-joined, encoded name to both URLs', () => {
  assert.deepEqual(organicMapsLinks(47.3859, 8.5766, 'Zoo Zürich'), {
    scheme: 'om://47.385900,8.576600/Zoo_Z%C3%BCrich',
    fallback: 'https://omaps.app/47.385900,8.576600/Zoo_Z%C3%BCrich',
  });
});

test('organicMapsLinks trims whitespace and collapses internal runs', () => {
  assert.deepEqual(organicMapsLinks(1, 2, '  W1ABC-9  '), {
    scheme: 'om://1.000000,2.000000/W1ABC-9',
    fallback: 'https://omaps.app/1.000000,2.000000/W1ABC-9',
  });
});

test('googleMapsUrl uses the documented search API format', () => {
  assert.equal(
    googleMapsUrl(55.751809, 37.6130029),
    'https://www.google.com/maps/search/?api=1&query=55.751809,37.613003',
  );
});

test('appleMapsLinks omits q= when no name is given', () => {
  assert.deepEqual(appleMapsLinks(-33.8688, 151.2093), {
    scheme: 'maps://?ll=-33.868800,151.209300',
    fallback: 'https://maps.apple.com/?ll=-33.868800,151.209300',
  });
});

test('appleMapsLinks encodes a name into q= on both URLs', () => {
  assert.deepEqual(appleMapsLinks(38.970559, -9.419289, 'W1ABC-9'), {
    scheme: 'maps://?ll=38.970559,-9.419289&q=W1ABC-9',
    fallback: 'https://maps.apple.com/?ll=38.970559,-9.419289&q=W1ABC-9',
  });
});

test('coordinates are formatted to 6 decimal places regardless of input precision', () => {
  assert.equal(organicMapsLinks(1, 2).scheme, 'om://1.000000,2.000000');
  assert.equal(googleMapsUrl(1.23456789, 2.3), 'https://www.google.com/maps/search/?api=1&query=1.234568,2.300000');
});

