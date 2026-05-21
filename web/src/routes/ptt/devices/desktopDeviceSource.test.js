// web/src/routes/ptt/devices/desktopDeviceSource.test.js
import { test } from 'node:test';
import assert from 'node:assert/strict';

test('desktopDeviceSource.list returns devices from /api/ptt/available filtered by method', async () => {
  const fakeApi = {
    get: async (path) => {
      assert.equal(path, '/ptt/available');
      return [
        { path: '/dev/ttyUSB0', type: 'serial', recommended: true },
        { path: '/dev/gpiochip0', type: 'gpio', recommended: true },
        { path: '/dev/hidraw0', type: 'cm108', recommended: true },
      ];
    },
  };
  const { createDesktopDeviceSource } = await import('./desktopDeviceSource.js');
  const src = createDesktopDeviceSource(fakeApi);

  const serial = await src.list({ wire: { method: 'serial_rts' } });
  assert.deepEqual(serial.map(d => d.path), ['/dev/ttyUSB0']);

  const gpio = await src.list({ wire: { method: 'gpio' } });
  assert.deepEqual(gpio.map(d => d.path), ['/dev/gpiochip0']);

  const cm108 = await src.list({ wire: { method: 'cm108' } });
  assert.deepEqual(cm108.map(d => d.path), ['/dev/hidraw0']);
});

test('desktopDeviceSource.list for rigctld returns an empty array (no devices to pick)', async () => {
  const fakeApi = { get: async () => [{ path: '/dev/ttyUSB0', type: 'serial', recommended: true }] };
  const { createDesktopDeviceSource } = await import('./desktopDeviceSource.js');
  const src = createDesktopDeviceSource(fakeApi);
  const rig = await src.list({ wire: { method: 'rigctld' } });
  assert.deepEqual(rig, []);
});

test('desktopDeviceSource.list for none returns an empty array', async () => {
  const fakeApi = { get: async () => [{ path: '/dev/ttyUSB0', type: 'serial', recommended: true }] };
  const { createDesktopDeviceSource } = await import('./desktopDeviceSource.js');
  const src = createDesktopDeviceSource(fakeApi);
  assert.deepEqual(await src.list({ wire: { method: 'none' } }), []);
});

test('desktopDeviceSource has no requestPermission (desktop devices need no permission)', async () => {
  const { createDesktopDeviceSource } = await import('./desktopDeviceSource.js');
  const src = createDesktopDeviceSource({ get: async () => [] });
  assert.equal(src.requestPermission, undefined);
});
