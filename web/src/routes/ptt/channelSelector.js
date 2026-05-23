// web/src/routes/ptt/channelSelector.js
//
// Channel-selector auto-hide rule, generic across desktop and Android.
// PTT applies to modem-backed channels (input_device_id != null). When
// exactly one modem-backed channel still needs a PttConfig row, the
// add-flow auto-binds to it and the selector UI is omitted.

export function modemBackedChannels(channels) {
  return (channels || []).filter(c => c.input_device_id != null);
}

export function channelsNeedingPtt(channels, pttByChannel) {
  const map = pttByChannel || new Map();
  return modemBackedChannels(channels).filter(c => !map.has(c.id));
}

export function showChannelSelector(channels, pttByChannel) {
  return channelsNeedingPtt(channels, pttByChannel).length > 1;
}

export function showAddButton(channels, pttByChannel) {
  return channelsNeedingPtt(channels, pttByChannel).length > 0;
}

// Why the detected-hardware cards can't be configured, or null when at
// least one channel can accept a new PttConfig. The UI uses this to disable
// the device cards and show inline guidance instead of letting a click fall
// through to a transient error toast.
//   null               -> configurable, cards are actionable
//   'no-modem-channel' -> no audio-modem channel exists (zero channels, or
//                         only KISS-TNC channels with input_device_id == null)
//   'all-configured'   -> every modem-backed channel already has a PttConfig
export function pttDetectionBlockedReason(channels, pttByChannel) {
  if (channelsNeedingPtt(channels, pttByChannel).length > 0) return null;
  if (modemBackedChannels(channels).length === 0) return 'no-modem-channel';
  return 'all-configured';
}
