// Groups enabled beacons by the channel card they should show a
// "Beacon Now" button on, for Dashboard.svelte.
//
// Channel 0 (Auto) beacons are bucketed under firstTxChannelId -- the
// same channel App.resolveTxChannel(ctx, 0) would pick -- so the
// button appears on the channel that will actually transmit instead
// of silently vanishing (no real channel card ever has id 0).
export function groupBeaconsByChannel(beacons, firstTxChannelId) {
  const acc = {};
  for (const b of beacons || []) {
    if (!b?.enabled) continue;
    const key = b.channel === 0 ? firstTxChannelId : b.channel;
    // No TX-capable channel to auto-resolve to yet: drop the beacon
    // from the grouping rather than bucket it under a null/undefined
    // key, matching that it would also fail to transmit today.
    if (key == null) continue;
    (acc[key] ??= []).push(b);
  }
  return acc;
}
