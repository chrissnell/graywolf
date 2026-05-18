// Unit tests for the Android PTT SPA additions (T12).
// Covers: postChannelPtt API helper, press-and-hold heartbeat dispatch,
// USB device role matching.
//
// Mirror the node:test style of Channels.convert.test.js.
// These tests exercise pure logic extracted from Channels.svelte —
// the Svelte component itself has no headless harness in this repo.

import { test, before, after } from 'node:test';
import assert from 'node:assert/strict';

// ---------------------------------------------------------------------------
// postChannelPtt — API helper
// ---------------------------------------------------------------------------
// Import the helper after wiring a fetch mock so the module sees globalThis.fetch.

before(() => {
  globalThis.window = globalThis.window ?? { location: { hash: '' } };
  // Provide a working GraywolfWebInterface so the module-level bearer-token
  // path doesn't redirect.
  globalThis.GraywolfWebInterface = { getBearerToken: () => 'test-token' };
});

after(() => {
  delete globalThis.GraywolfWebInterface;
});

test('postChannelPtt POSTs keyed=true to /api/channels/{id}/ptt', async () => {
  const calls = [];
  globalThis.fetch = async (url, opts) => {
    calls.push({ url, opts });
    return new Response(null, { status: 204 });
  };

  const { postChannelPtt } = await import('../lib/api.js');
  await postChannelPtt(7, true);

  assert.equal(calls.length, 1);
  assert.ok(calls[0].url.endsWith('/api/channels/7/ptt'), `url was ${calls[0].url}`);
  assert.equal(calls[0].opts.method, 'POST');
  const body = JSON.parse(calls[0].opts.body);
  assert.equal(body.keyed, true);
});

test('postChannelPtt POSTs keyed=false to /api/channels/{id}/ptt', async () => {
  const calls = [];
  globalThis.fetch = async (url, opts) => {
    calls.push({ url, opts });
    return new Response(null, { status: 204 });
  };

  const { postChannelPtt } = await import('../lib/api.js');
  await postChannelPtt(3, false);

  assert.equal(calls.length, 1);
  assert.ok(calls[0].url.endsWith('/api/channels/3/ptt'));
  const body = JSON.parse(calls[0].opts.body);
  assert.equal(body.keyed, false);
});

// ---------------------------------------------------------------------------
// Press-and-hold heartbeat logic
// ---------------------------------------------------------------------------
// Extracted as a pure function matching the Channels.svelte behaviour so
// it can be tested without mounting the Svelte component.

/**
 * simulateTestPttHold sets up the press-and-hold behaviour using the
 * supplied postFn, starts the 2-second heartbeat, and returns { stop }.
 * Mirrors startTestPtt / stopTestPtt in Channels.svelte.
 */
function simulateTestPttHold(channelId, postFn) {
  let held = true;
  let interval = null;

  // Initial key call
  postFn(channelId, true).catch(() => { held = false; });

  // Heartbeat every 2 s
  interval = setInterval(() => {
    if (!held) return;
    postFn(channelId, true).catch(() => {
      clearInterval(interval);
      interval = null;
      held = false;
    });
  }, 2000);

  return {
    stop: async () => {
      if (interval !== null) {
        clearInterval(interval);
        interval = null;
      }
      held = false;
      await postFn(channelId, false);
    },
  };
}

test('pointerdown calls postChannelPtt(id, true); pointerup calls postChannelPtt(id, false)', async (t) => {
  // Use mock.timers to control setInterval without real delay.
  t.mock.timers.enable({ apis: ['setInterval'] });

  const calls = [];
  const mockPost = async (id, keyed) => { calls.push({ id, keyed }); };

  const { stop } = simulateTestPttHold(5, mockPost);

  // pointerdown: initial keyed=true should have been called
  assert.deepEqual(calls, [{ id: 5, keyed: true }]);

  // pointerup: stop clears interval and sends keyed=false
  await stop();
  assert.deepEqual(calls, [
    { id: 5, keyed: true },
    { id: 5, keyed: false },
  ]);
});

test('heartbeat re-sends keyed=true after 2 seconds while held', async (t) => {
  t.mock.timers.enable({ apis: ['setInterval'] });

  const calls = [];
  const mockPost = async (id, keyed) => { calls.push({ id, keyed }); };

  const { stop } = simulateTestPttHold(5, mockPost);
  assert.deepEqual(calls, [{ id: 5, keyed: true }]);

  // Advance clock by 2 s — heartbeat fires once
  t.mock.timers.tick(2000);
  // Wait a tick for the async postFn inside setInterval to resolve
  await Promise.resolve();
  assert.deepEqual(calls, [
    { id: 5, keyed: true },
    { id: 5, keyed: true },
  ]);

  // Advance by another 2 s — second heartbeat
  t.mock.timers.tick(2000);
  await Promise.resolve();
  assert.deepEqual(calls, [
    { id: 5, keyed: true },
    { id: 5, keyed: true },
    { id: 5, keyed: true },
  ]);

  await stop();
  // Final keyed=false on release
  assert.equal(calls[calls.length - 1].keyed, false);
});

// ---------------------------------------------------------------------------
// USB device role matching
// ---------------------------------------------------------------------------
// Pure matching logic matching Channels.svelte's PTT_METHOD_USB_ROLE table.

const PTT_METHOD_CP2102N_RTS  = 1;
const PTT_METHOD_CM108_HID    = 2;
const PTT_METHOD_AIOC_CDC_DTR = 3;
const PTT_METHOD_VOX          = 4;

const PTT_METHOD_USB_ROLE = {
  [PTT_METHOD_CP2102N_RTS]:  'CP2102N',
  [PTT_METHOD_AIOC_CDC_DTR]: 'AIOC',
  [PTT_METHOD_CM108_HID]:    'CM108',
};

function pickUsbDevice(devices, pttMethod) {
  const role = PTT_METHOD_USB_ROLE[pttMethod];
  return role ? (devices.find(d => d.role === role) || null) : null;
}

const fakeDevices = [
  { name: 'Digirig', role: 'CP2102N', vid: 0x10C4, pid: 0xEA60, vid_hex: '0x10C4', pid_hex: '0xEA60', permission_granted: true },
  { name: 'AIOC',    role: 'AIOC',    vid: 0x1209, pid: 0x7388, vid_hex: '0x1209', pid_hex: '0x7388', permission_granted: false },
];

test('USB device match: CP2102N_RTS picks Digirig not AIOC', () => {
  const device = pickUsbDevice(fakeDevices, PTT_METHOD_CP2102N_RTS);
  assert.equal(device?.name, 'Digirig');
  assert.equal(device?.role, 'CP2102N');
});

test('USB device match: AIOC_CDC_DTR picks AIOC not Digirig', () => {
  const device = pickUsbDevice(fakeDevices, PTT_METHOD_AIOC_CDC_DTR);
  assert.equal(device?.name, 'AIOC');
  assert.equal(device?.role, 'AIOC');
});

test('USB device match: CM108_HID returns null when no CM108 present', () => {
  const device = pickUsbDevice(fakeDevices, PTT_METHOD_CM108_HID);
  assert.equal(device, null);
});

test('USB device match: VOX returns null (no USB role for VOX)', () => {
  const device = pickUsbDevice(fakeDevices, PTT_METHOD_VOX);
  assert.equal(device, null);
});

test('USB device match: CM108_HID picks the right device when present', () => {
  const devicesWithCm108 = [
    ...fakeDevices,
    { name: 'CM108B dongle', role: 'CM108', vid: 0x0D8C, pid: 0x0012, vid_hex: '0x0D8C', pid_hex: '0x0012', permission_granted: true },
  ];
  const device = pickUsbDevice(devicesWithCm108, PTT_METHOD_CM108_HID);
  assert.equal(device?.name, 'CM108B dongle');
});
