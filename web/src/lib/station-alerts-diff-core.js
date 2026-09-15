// Pure diffing helper for stationAlertsTransport.js: on each poll of
// GET /api/stations/alerts, decides which rows are "newly alerting" so
// a notification fires once per transition into Emergency rather than
// on every poll while a station remains in that state. A callsign that
// clears and later re-asserts Emergency is treated as newly alerting
// again -- mirrors bulletins-diff-core.js's diffNewlyUnread shape.

/**
 * @param {Set<string>} prevActive callsigns that were alerting on the previous poll
 * @param {Array<{callsign: string}>} freshRows latest GET /stations/alerts rows
 * @returns {Array<object>} the subset of freshRows not present in prevActive
 */
export function diffNewlyAlerting(prevActive, freshRows) {
  const newly = [];
  for (const row of freshRows || []) {
    if (!row || !row.callsign) continue;
    if (!prevActive.has(row.callsign)) newly.push(row);
  }
  return newly;
}
