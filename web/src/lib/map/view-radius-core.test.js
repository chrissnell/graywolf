// Tests for the pure "N miles around a center point" math behind the
// Live Map's "Reset to default view" button.
//
// Run with:
//   node --test src/lib/map/view-radius-core.test.js

import { strict as assert } from 'node:assert';
import { describe, it } from 'node:test';

import {
  boundsAroundMiles,
  parseViewRadiusMiles,
  DEFAULT_VIEW_RADIUS_MILES,
  MIN_VIEW_RADIUS_MILES,
  MAX_VIEW_RADIUS_MILES,
} from './view-radius-core.js';

describe('boundsAroundMiles', () => {
  it('produces a symmetric box around the center at the equator', () => {
    const b = boundsAroundMiles(0, 0, 69);
    // At the equator, 1 degree of lat and lon both cover ~69 miles.
    assert.ok(Math.abs(b.north - 1) < 0.01);
    assert.ok(Math.abs(b.south - -1) < 0.01);
    assert.ok(Math.abs(b.east - 1) < 0.01);
    assert.ok(Math.abs(b.west - -1) < 0.01);
  });

  it('widens the longitude span at higher latitudes to cover the same real-world distance', () => {
    const equator = boundsAroundMiles(0, 0, 20);
    const midLat = boundsAroundMiles(45, 0, 20);
    const lonSpanEquator = equator.east - equator.west;
    const lonSpanMidLat = midLat.east - midLat.west;
    assert.ok(lonSpanMidLat > lonSpanEquator);
  });

  it('stays finite near the poles', () => {
    const b = boundsAroundMiles(89.9, 0, 20);
    assert.ok(Number.isFinite(b.east));
    assert.ok(Number.isFinite(b.west));
  });

  it('is centered on the given lat/lon', () => {
    const b = boundsAroundMiles(34.5, -84.3, 20);
    assert.ok(Math.abs((b.north + b.south) / 2 - 34.5) < 1e-9);
    assert.ok(Math.abs((b.east + b.west) / 2 - -84.3) < 1e-9);
  });
});

describe('parseViewRadiusMiles', () => {
  it('defaults to DEFAULT_VIEW_RADIUS_MILES when never stored', () => {
    assert.equal(parseViewRadiusMiles(null), DEFAULT_VIEW_RADIUS_MILES);
    assert.equal(parseViewRadiusMiles(undefined), DEFAULT_VIEW_RADIUS_MILES);
  });

  it('round-trips a valid value', () => {
    assert.equal(parseViewRadiusMiles('50'), 50);
    assert.equal(parseViewRadiusMiles('2.5'), 2.5);
  });

  it('falls back to the default for garbage or non-positive values', () => {
    assert.equal(parseViewRadiusMiles('garbage'), DEFAULT_VIEW_RADIUS_MILES);
    assert.equal(parseViewRadiusMiles('0'), DEFAULT_VIEW_RADIUS_MILES);
    assert.equal(parseViewRadiusMiles('-5'), DEFAULT_VIEW_RADIUS_MILES);
  });

  it('clamps to [MIN_VIEW_RADIUS_MILES, MAX_VIEW_RADIUS_MILES]', () => {
    assert.equal(parseViewRadiusMiles('0.001'), MIN_VIEW_RADIUS_MILES);
    assert.equal(parseViewRadiusMiles('99999'), MAX_VIEW_RADIUS_MILES);
  });
});
