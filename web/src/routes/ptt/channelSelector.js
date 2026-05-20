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
