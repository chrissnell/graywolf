// Trailing-window packet rate computation for the dashboard RX/TX cards.
// Pure JS — no runes, no DOM — so the windowing and reset logic can be tested
// independently of Dashboard.svelte.

/**
 * Append a cumulative-counter sample and compute the packets/hour rate over a
 * trailing window.
 *
 * Samples hold absolute counter readings ({ t, rx, tx }); the rate is the
 * delta between the newest and oldest in-window sample divided by their
 * elapsed wall-clock time. Counters that move backwards (a station restart
 * resets them) discard the stale window and start measuring fresh.
 *
 * @param {Array<{t:number,rx:number,tx:number}>} samples - prior samples, oldest first
 * @param {{t:number,rx:number,tx:number}} sample - the new reading
 * @param {number} windowMs - trailing window length in milliseconds
 * @returns {{samples:Array, rxRate:(number|null), txRate:(number|null)}}
 *   The pruned sample list and the rates in packets/hour, null until 2+ samples
 *   span a non-zero interval.
 */
export function pushRateSample(samples, sample, windowMs) {
  let next = samples;
  const last = next[next.length - 1];
  if (last && (sample.rx < last.rx || sample.tx < last.tx)) next = [];

  next = next.concat(sample).filter((s) => s.t >= sample.t - windowMs);

  const oldest = next[0];
  const elapsedMs = sample.t - oldest.t;
  if (next.length < 2 || elapsedMs <= 0) {
    return { samples: next, rxRate: null, txRate: null };
  }
  return {
    samples: next,
    rxRate: ((sample.rx - oldest.rx) * 3600000) / elapsedMs,
    txRate: ((sample.tx - oldest.tx) * 3600000) / elapsedMs,
  };
}
