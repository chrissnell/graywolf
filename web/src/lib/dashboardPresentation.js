// Display-only distinctions: never infer modem measurements from zero-filled
// KISS status fields, and never change a beacon's configured delivery route.
export function hasNativeInput(channel) {
  return Number(channel?.input_device_id) > 0;
}
export function channelAudioPeak(channel) {
  if (!hasNativeInput(channel)) return null;
  const value = channel.device_peak_dbfs ?? channel.audio_peak;
  return typeof value === 'number' && Number.isFinite(value) ? value : null;
}
export function dashboardBeacons(beacons = []) {
  return beacons.reduce((groups, beacon) => {
    if (beacon.enabled && beacon.send_path !== 'is_only' && Number(beacon.channel) > 0) {
      (groups[beacon.channel] ??= []).push(beacon);
    }
    return groups;
  }, {});
}
export function kissChannelStatus(channel, interfaces, available = true) {
  if (!available) return { label: 'KISS status unavailable', connected: false, txReady: false };
  const linked = interfaces.filter(item => item.enabled && item.mode === 'tnc' && Number(item.channel) === Number(channel.id));
  const connected = linked.some(item => item.state === 'connected');
  return {
    label: connected ? 'KISS connected' : linked.some(item => item.state === 'listening') ? 'KISS listening' : linked.length ? 'KISS disconnected' : 'No enabled KISS TNC',
    connected,
    txReady: linked.some(item => item.state === 'connected' && item.allow_tx_from_governor),
  };
}
