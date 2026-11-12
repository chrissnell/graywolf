// Link state comes from the transport supervisor, never from packet counts.
export function kissLinkStatus(iface, available = true) {
  if (!available) return { label: 'Status unavailable', tone: 'unknown' };
  if (!iface.enabled) return { label: 'Disabled', tone: 'disabled' };
  switch (iface.state) {
    case 'connected': return { label: 'Connected', tone: 'connected' };
    case 'connecting': return { label: 'Connecting', tone: 'pending' };
    case 'backoff': return { label: 'Reconnecting', tone: 'pending' };
    // A listening socket does not prove that a TNC has connected.
    case 'listening': return { label: 'Listening', tone: 'pending' };
    case 'disconnected':
    case 'stopped':
    case undefined:
    case '': return { label: 'Disconnected', tone: 'disconnected' };
    default: return { label: 'Status unavailable', tone: 'unknown' };
  }
}

export function kissTransportLabel(type) {
  return {
    'ble-device': 'BLE',
    'bluetooth': 'Bluetooth',
    'usbserial': 'USB',
    'serial': 'Serial',
    'tcp-client': 'TCP',
    'tcp': 'TCP listener',
  }[type] || 'TNC';
}
