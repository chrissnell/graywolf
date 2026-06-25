import test from 'node:test';
import assert from 'node:assert/strict';
import { pushRateSample } from './packetRate.js';

const WINDOW = 10 * 60 * 1000; // 10 minutes

test('pushRateSample: first sample has no rate yet', () => {
  const r = pushRateSample([], { t: 0, rx: 100, tx: 5 }, WINDOW);
  assert.equal(r.rxRate, null);
  assert.equal(r.txRate, null);
  assert.equal(r.samples.length, 1);
});

test('pushRateSample: rate is delta over elapsed, scaled to per-hour', () => {
  // 60 RX / 6 TX over 60s => 3600 RX/hr, 360 TX/hr.
  let s = [];
  ({ samples: s } = pushRateSample(s, { t: 0, rx: 100, tx: 10 }, WINDOW));
  const r = pushRateSample(s, { t: 60000, rx: 160, tx: 16 }, WINDOW);
  assert.equal(r.rxRate, 3600);
  assert.equal(r.txRate, 360);
});

test('pushRateSample: drops samples outside the trailing window', () => {
  let s = [];
  ({ samples: s } = pushRateSample(s, { t: 0, rx: 0, tx: 0 }, WINDOW));
  ({ samples: s } = pushRateSample(s, { t: 5 * 60000, rx: 300, tx: 0 }, WINDOW));
  // 11 minutes after the first sample: the t=0 reading falls out of the window.
  const r = pushRateSample(s, { t: 11 * 60000, rx: 900, tx: 0 }, WINDOW);
  assert.ok(!r.samples.some((x) => x.t === 0), 'oldest out-of-window sample pruned');
  // Window now spans t=5min..11min: 600 packets over 360s => 6000/hr.
  assert.equal(r.rxRate, 6000);
});

test('pushRateSample: counter reset (backwards) restarts the window', () => {
  let s = [];
  ({ samples: s } = pushRateSample(s, { t: 0, rx: 1000, tx: 50 }, WINDOW));
  ({ samples: s } = pushRateSample(s, { t: 60000, rx: 1100, tx: 55 }, WINDOW));
  // Station restarted: counters dropped. Window resets; no rate from one sample.
  const r = pushRateSample(s, { t: 120000, rx: 5, tx: 0 }, WINDOW);
  assert.deepEqual(r.samples, [{ t: 120000, rx: 5, tx: 0 }]);
  assert.equal(r.rxRate, null);
  assert.equal(r.txRate, null);
});

test('pushRateSample: zero traffic yields a real 0/hr, not null', () => {
  let s = [];
  ({ samples: s } = pushRateSample(s, { t: 0, rx: 42, tx: 7 }, WINDOW));
  const r = pushRateSample(s, { t: 30000, rx: 42, tx: 7 }, WINDOW);
  assert.equal(r.rxRate, 0);
  assert.equal(r.txRate, 0);
});

test('pushRateSample: duplicate timestamp does not divide by zero', () => {
  let s = [];
  ({ samples: s } = pushRateSample(s, { t: 1000, rx: 0, tx: 0 }, WINDOW));
  const r = pushRateSample(s, { t: 1000, rx: 10, tx: 1 }, WINDOW);
  assert.equal(r.rxRate, null);
  assert.equal(r.txRate, null);
});
