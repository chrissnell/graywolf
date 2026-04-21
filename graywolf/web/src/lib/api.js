// Thin API client wrapping all fetch calls to /api/*.
// Returns mock data when the API is unreachable (dev without backend).

const MOCK_DELAY = 200;

// ApiError carries the structured body from a non-2xx response so
// callers that need richer context than `err.message` (e.g. the
// Phase 5 channel-delete 409 with a referrers list) can read it
// without an extra fetch. Non-409 paths still present as plain
// Error-compatible objects since they only carry {error} strings.
export class ApiError extends Error {
  constructor(status, body) {
    const message = (body && body.error) || `HTTP ${status}`;
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.body = body || null;
  }
}

async function request(method, path, body = null) {
  const opts = {
    method,
    credentials: 'same-origin',
    headers: {},
  };
  if (body !== null) {
    opts.headers['Content-Type'] = 'application/json';
    opts.body = JSON.stringify(body);
  }
  let res;
  try {
    res = await fetch(`/api${path}`, opts);
  } catch {
    // Genuine network failure (dev without backend, DNS, CORS). HTTP
    // errors from a reachable backend must NOT fall through here —
    // they need to surface so pages can render their error state.
    return getMockData(method, path, body);
  }
  if (res.status === 401) {
    window.location.hash = '#/login';
    throw new ApiError(401, { error: 'Unauthorized' });
  }
  if (!res.ok) {
    const errBody = await res.json().catch(() => ({ error: res.statusText }));
    throw new ApiError(res.status, errBody);
  }
  if (res.status === 204) return null;
  return res.json();
}

export const api = {
  get: (path) => request('GET', path),
  post: (path, body) => request('POST', path, body),
  put: (path, body) => request('PUT', path, body),
  delete: (path) => request('DELETE', path),
};

// --- Mock data for development without backend ---

function delay(data) {
  return new Promise((r) => setTimeout(() => r(data), MOCK_DELAY));
}

const mockChannels = [
  { id: 1, name: 'VHF APRS', frequency: '144.390', modem_type: 'afsk1200', baud_rate: 1200, device: 'hw:0', enabled: true },
  { id: 2, name: '9600 Data', frequency: '445.925', modem_type: 'gfsk9600', baud_rate: 9600, device: 'hw:1', enabled: false },
];

const mockAudioDevices = [
  { id: 1, name: 'USB Sound Card', device_path: 'hw:0,0', sample_rate: 48000, channels: 1 },
];

const mockAvailableDevices = [
  { name: 'USB Audio CODEC', path: 'hw:0,0', sample_rates: [8000, 16000, 44100, 48000], channels: [1, 2] },
  { name: 'Built-in Audio', path: 'hw:1,0', sample_rates: [44100, 48000, 96000], channels: [2] },
];

const mockPtt = [
  { id: 1, channel_id: 1, method: 'serial_rts', device_path: '/dev/ttyUSB0', gpio_pin: 0 },
];

const mockPttAvailable = [
  { path: '/dev/ttyUSB0', type: 'serial', name: 'ttyUSB0' },
  { path: '/dev/ttyACM0', type: 'serial', name: 'ttyACM0' },
];

const mockKiss = [
  { id: 1, type: 'tcp', tcp_port: 8001, serial_device: '', baud_rate: 0 },
];

const mockAgw = { tcp_port: 8000, monitor_port: 8002, enabled: true };

const mockIgate = {
  enabled: true, server: 'rotate.aprs2.net', port: 14580,
  callsign: 'N0CALL-10', passcode: '12345', server_filter: 'r/35.0/-106.0/100',
};

const mockIgateFilters = [
  { id: 1, type: 'prefix', pattern: 'W5', action: 'allow', priority: 100, enabled: true },
];

const mockDigipeater = {
  id: 1, enabled: false, my_call: 'N0CALL-1', dedupe_window_seconds: 30,
};

const mockDigipeaterRules = [
  { id: 1, from_channel: 1, to_channel: 1, alias: 'WIDE', alias_type: 'widen', max_hops: 1, action: 'repeat', priority: 100, enabled: true },
  { id: 2, from_channel: 1, to_channel: 1, alias: 'WIDE', alias_type: 'widen', max_hops: 2, action: 'repeat', priority: 100, enabled: true },
];

const mockBeacons = [
  { id: 1, channel: 1, callsign: 'N0CALL-9', destination: 'APGRWO', path: 'WIDE1-1,WIDE2-1', comment: 'graywolf', interval: 600, enabled: true },
];

const mockGps = { source: 'serial', serial_port: '/dev/ttyACM0', baud_rate: 9600, gpsd_host: 'localhost', gpsd_port: 2947 };

const mockPackets = [
  { id: 1, timestamp: new Date().toISOString(), source: 'N0CALL-9', destination: 'APRS', path: 'WIDE1-1', type: 'position', raw: 'N0CALL-9>APRS,WIDE1-1:!3500.00N/10600.00W-PHG2360', direction: 'rx', channel: 'VHF APRS' },
  { id: 2, timestamp: new Date(Date.now() - 5000).toISOString(), source: 'W5ABC-7', destination: 'APGRWO', path: 'WIDE2-1', type: 'position', raw: 'W5ABC-7>APGRWO,WIDE2-1:@092345z3501.00N/10601.00W_090/005', direction: 'rx', channel: 'VHF APRS' },
];

const mockPosition = { valid: true, lat: 35.0, lon: -106.0, alt_m: 1500, has_alt: true, speed_kt: 0, heading_deg: 0, has_course: false };

const mockSimulation = { enabled: false, packets: mockPackets };

const mockStatus = {
  uptime_seconds: 3600,
  channels: [
    { id: 1, name: 'VHF APRS', modem_type: 'afsk', bit_rate: 1200, rx_frames: 142, tx_frames: 23, dcd_state: false, audio_peak: -12.0, input_device_id: 1, device_peak_dbfs: -18.0, device_rms_dbfs: -24.0, device_clipping: false },
    { id: 2, name: '9600 Data', modem_type: 'gfsk', bit_rate: 9600, rx_frames: 0, tx_frames: 0, dcd_state: false, audio_peak: 0, input_device_id: 1, device_peak_dbfs: 0, device_rms_dbfs: 0, device_clipping: false },
  ],
  igate: { connected: true, server: 'rotate.aprs2.net', callsign: 'N0CALL-10', rf_to_is_gated: 89, is_to_rf_gated: 0, packets_filtered: 12, rf_to_is_dropped: 0 },
};

function getMockData(method, path, body) {
  // Auth
  if (path === '/auth/login' && method === 'POST') return delay({ ok: true });
  if (path === '/auth/logout' && method === 'POST') return delay({ ok: true });
  if (path === '/auth/setup' && method === 'GET') return delay({ needs_setup: true });
  if (path === '/auth/setup' && method === 'POST') return delay({ ok: true });

  // Channels
  if (path === '/channels' && method === 'GET') return delay(mockChannels);
  if (path === '/channels' && method === 'POST') return delay({ id: 3, ...body });
  if (path.match(/^\/channels\/\d+$/) && method === 'GET') return delay(mockChannels[0]);
  if (path.match(/^\/channels\/\d+$/) && method === 'PUT') return delay(body);
  if (path.match(/^\/channels\/\d+$/) && method === 'DELETE') return delay(null);
  if (path.match(/^\/channels\/\d+\/stats$/)) return delay(mockStatus.channels[0]);

  // Audio devices
  if (path === '/audio-devices' && method === 'GET') return delay(mockAudioDevices);
  if (path === '/audio-devices' && method === 'POST') return delay({ id: 2, ...body });
  if (path === '/audio-devices/available') return delay(mockAvailableDevices);
  if (path === '/audio-devices/levels') return delay({ 1: { device_id: 1, peak_dbfs: -18 + Math.random() * 6, rms_dbfs: -24 + Math.random() * 6, clipping: false } });
  if (path.match(/^\/audio-devices\/\d+\/test-tone$/) && method === 'POST') return delay({ status: 'ok' });
  if (path.match(/^\/audio-devices\/\d+\/gain$/) && method === 'PUT') return delay(body);
  if (path.match(/^\/audio-devices\/\d+$/) && method === 'PUT') return delay(body);
  if (path.match(/^\/audio-devices\/\d+$/) && method === 'DELETE') return delay(null);

  // PTT
  if (path === '/ptt' && method === 'GET') return delay(mockPtt);
  if (path === '/ptt' && method === 'POST') return delay({ id: 2, ...body });
  if (path === '/ptt/available') return delay(mockPttAvailable);
  if (path.match(/^\/ptt\/\d+$/) && method === 'PUT') return delay(body);
  if (path.match(/^\/ptt\/\d+$/) && method === 'DELETE') return delay(null);

  // TX Timing (used by channel editor)
  if (path === '/tx-timing' && method === 'GET') return delay([]);
  if (path.match(/^\/tx-timing\/\d+$/) && method === 'PUT') return delay(body);

  // KISS
  if (path === '/kiss' && method === 'GET') return delay(mockKiss);
  if (path === '/kiss' && method === 'POST') return delay({ id: 2, ...body });
  if (path.match(/^\/kiss\/\d+$/) && method === 'PUT') return delay(body);
  if (path.match(/^\/kiss\/\d+$/) && method === 'DELETE') return delay(null);

  // AGW
  if (path === '/agw' && method === 'GET') return delay(mockAgw);
  if (path === '/agw' && method === 'PUT') return delay(body);

  // iGate
  if (path === '/igate/config' && method === 'GET') return delay(mockIgate);
  if (path === '/igate/config' && method === 'PUT') return delay(body);
  if (path === '/igate/filters' && method === 'GET') return delay(mockIgateFilters);
  if (path === '/igate/filters' && method === 'POST') return delay({ id: 2, ...body });
  if (path.match(/^\/igate\/filters\/\d+$/) && method === 'PUT') return delay(body);
  if (path.match(/^\/igate\/filters\/\d+$/) && method === 'DELETE') return delay(null);

  // Digipeater
  if (path === '/digipeater' && method === 'GET') return delay(mockDigipeater);
  if (path === '/digipeater' && method === 'PUT') return delay(body);
  if (path === '/digipeater/rules' && method === 'GET') return delay(mockDigipeaterRules);
  if (path === '/digipeater/rules' && method === 'POST') return delay({ id: 3, ...body });
  if (path.match(/^\/digipeater\/rules\/\d+$/) && method === 'PUT') return delay(body);
  if (path.match(/^\/digipeater\/rules\/\d+$/) && method === 'DELETE') return delay(null);

  // Beacons
  if (path === '/beacons' && method === 'GET') return delay(mockBeacons);
  if (path === '/beacons' && method === 'POST') return delay({ id: 2, ...body });
  if (path.match(/^\/beacons\/\d+$/) && method === 'PUT') return delay(body);
  if (path.match(/^\/beacons\/\d+$/) && method === 'DELETE') return delay(null);
  if (path.match(/^\/beacons\/\d+\/send$/) && method === 'POST') return delay({ status: 'sent' });

  // GPS
  if (path === '/gps' && method === 'GET') return delay(mockGps);
  if (path === '/gps' && method === 'PUT') return delay(body);

  // Status (aggregated dashboard data)
  if (path === '/status') return delay(mockStatus);

  // Packets
  if (path.startsWith('/packets')) return delay(mockPackets);
  if (path === '/position') return delay(mockPosition);

  // Simulation
  if (path === '/simulation' && method === 'GET') return delay(mockSimulation);
  if (path === '/simulation' && method === 'PUT') return delay(body);

  return delay(null);
}
