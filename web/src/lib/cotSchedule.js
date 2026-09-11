// Pure JS mirror of pkg/cot.ComputeOffsets (Go) -- keep the two in sync;
// both are exercised against the same confirmed example (4 transmits,
// 300s second delay, decay factor 2 -> 0s/300s/600s/1200s).
//
// Drives the live schedule-preview table on the Cursor-on-Target
// Settings tab (BeaconSettings.svelte) so an operator can see exactly
// when each retransmit will fire before saving.

/**
 * One row of the computed CoT retransmit schedule.
 * @typedef {{ n: number, offsetSeconds: number, hms: string }} CotScheduleRow
 */

/**
 * Computes the cumulative time-from-first-send offset for each of
 * numTransmits total sends. TX1 fires immediately (offset 0); TX2
 * fires after secondDelaySeconds; every later TX's offset is the
 * previous TX's offset multiplied by decayFactor.
 *
 * @param {number} numTransmits
 * @param {number} secondDelaySeconds
 * @param {number} decayFactor
 * @returns {CotScheduleRow[]} empty when numTransmits <= 0
 */
export function computeCotSchedule(numTransmits, secondDelaySeconds, decayFactor) {
  const n = Math.floor(numTransmits) || 0;
  if (n <= 0) return [];
  const offsets = new Array(n).fill(0);
  if (n > 1) offsets[1] = secondDelaySeconds;
  for (let i = 2; i < n; i++) {
    offsets[i] = offsets[i - 1] * decayFactor;
  }
  return offsets.map((offsetSeconds, i) => ({
    n: i + 1,
    offsetSeconds,
    hms: formatHMS(offsetSeconds),
  }));
}

/** Formats a whole-seconds offset as zero-padded HH:MM:SS. */
export function formatHMS(totalSeconds) {
  const s = Math.max(0, Math.round(totalSeconds || 0));
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const sec = s % 60;
  const pad = (v) => String(v).padStart(2, '0');
  return `${pad(h)}:${pad(m)}:${pad(sec)}`;
}
