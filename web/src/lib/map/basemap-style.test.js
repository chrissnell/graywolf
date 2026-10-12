import { test } from 'node:test';
import assert from 'node:assert/strict';
import { selectBasemapStyle } from './basemap-style.js';

const GW = { name: 'graywolf' };
const OSM = { name: 'osm' };

test('uses the fallback without trying graywolf when not selected', async () => {
  let tried = false;
  const style = await selectBasemapStyle({
    useGraywolf: false,
    buildGraywolf: async () => {
      tried = true;
      return GW;
    },
    buildFallback: () => OSM,
  });
  assert.equal(style, OSM);
  assert.equal(tried, false);
});

test('returns the graywolf style and reports recovery when it builds', async () => {
  let recovered = 0;
  let fellBack = 0;
  const style = await selectBasemapStyle({
    useGraywolf: true,
    buildGraywolf: async () => GW,
    buildFallback: () => OSM,
    onFallback: () => fellBack++,
    onRecovered: () => recovered++,
  });
  assert.equal(style, GW);
  assert.equal(recovered, 1);
  assert.equal(fellBack, 0);
});

test('falls back to OSM and passes the error when graywolf fails', async () => {
  const boom = new Error('fetch style: 502');
  let seen = null;
  let recovered = 0;
  const style = await selectBasemapStyle({
    useGraywolf: true,
    buildGraywolf: async () => {
      throw boom;
    },
    buildFallback: () => OSM,
    onFallback: (err) => {
      seen = err;
    },
    onRecovered: () => recovered++,
  });
  assert.equal(style, OSM);
  assert.equal(seen, boom);
  assert.equal(recovered, 0);
});

test('callbacks are optional', async () => {
  const style = await selectBasemapStyle({
    useGraywolf: true,
    buildGraywolf: async () => {
      throw new Error('nope');
    },
    buildFallback: () => OSM,
  });
  assert.equal(style, OSM);
});
