// web/src/routes/ptt/devices/androidDeviceSource.test.js
import { test, before, after } from 'node:test';
import assert from 'node:assert/strict';

before(() => {
  globalThis.window = globalThis.window ?? { location: { hash: '' } };
});

test('androidDeviceSource.list filters /api/ptt/available by method deviceClass', async () => {
  const fakeApi = {
    get: async (path) => {
      assert.equal(path, '/ptt/available');
      return [
        { path: '', name: 'CP2102N', type: 'usb-cp2102n', usb_vendor: '10c4', usb_product: 'ea60', recommended: true, has_permission: true },
        { path: '', name: 'AIOC',    type: 'usb-cdc-acm',  usb_vendor: '1209', usb_product: '7388', recommended: true, has_permission: false },
        { path: '', name: 'CM108',   type: 'usb-hid',      usb_vendor: '0d8c', usb_product: '013c', recommended: true, has_permission: true },
        { path: '', name: 'Other',   type: 'usb-other',    usb_vendor: 'ffff', usb_product: '0000', recommended: false },
      ];
    },
  };
  const { createAndroidDeviceSource } = await import('./androidDeviceSource.js');
  const src = createAndroidDeviceSource(fakeApi);

  const cp = await src.list({ wire: { method: 'android', ptt_method: 1 }, deviceClass: 'cp2102n' });
  assert.deepEqual(cp.map(d => d.name), ['CP2102N']);

  const aioc = await src.list({ wire: { method: 'android', ptt_method: 3 }, deviceClass: 'cdc-acm' });
  assert.deepEqual(aioc.map(d => d.name), ['AIOC']);

  const cm = await src.list({ wire: { method: 'android', ptt_method: 2 }, deviceClass: 'hid-cm108' });
  assert.deepEqual(cm.map(d => d.name), ['CM108']);
});

test('androidDeviceSource.list for VOX (no deviceClass) returns an empty array', async () => {
  const fakeApi = { get: async () => [{ name: 'CP2102N', type: 'usb-cp2102n', recommended: true }] };
  const { createAndroidDeviceSource } = await import('./androidDeviceSource.js');
  const src = createAndroidDeviceSource(fakeApi);
  assert.deepEqual(await src.list({ wire: { method: 'android', ptt_method: 4 } }), []);
});

test('androidDeviceSource.list synthesizes usb:VID:PID path when the device row has none', async () => {
  // Backend rows omit path on Android (no stable POSIX path). The adapter
  // must inject a deterministic identifier so DevicePicker.selectedPath
  // compares correctly and the saved device_path round-trips on re-open.
  const fakeApi = {
    get: async () => [
      { path: '', name: 'AIOC', type: 'usb-cdc-acm', usb_vendor: '1209', usb_product: '7388', recommended: true },
    ],
  };
  const { createAndroidDeviceSource } = await import('./androidDeviceSource.js');
  const src = createAndroidDeviceSource(fakeApi);
  const list = await src.list({ wire: { method: 'android', ptt_method: 3 }, deviceClass: 'cdc-acm' });
  assert.equal(list.length, 1);
  assert.equal(list[0].path, 'usb:1209:7388');
});

test('androidDeviceSource.list preserves a non-empty backend path when present', async () => {
  // If the backend ever populates path (e.g., the desktop branch on a
  // shared serial cable), the adapter must not clobber it.
  const fakeApi = {
    get: async () => [
      { path: '/dev/bus/usb/001/002', name: 'AIOC', type: 'usb-cdc-acm', usb_vendor: '1209', usb_product: '7388', recommended: true },
    ],
  };
  const { createAndroidDeviceSource } = await import('./androidDeviceSource.js');
  const src = createAndroidDeviceSource(fakeApi);
  const list = await src.list({ wire: { method: 'android', ptt_method: 3 }, deviceClass: 'cdc-acm' });
  assert.equal(list[0].path, '/dev/bus/usb/001/002');
});

test('androidDeviceSource.requestPermission calls the JS bridge and resolves to the granted boolean', async () => {
  const calls = [];
  globalThis.GraywolfWebInterface = {
    requestUsbPermission(vid, pid, callbackId) {
      calls.push({ vid, pid, callbackId });
      // Simulate Kotlin's evaluateJavascript("__usbResult(id, true)") call.
      setTimeout(() => { globalThis.__usbResult(callbackId, true); }, 0);
    },
  };

  const { createAndroidDeviceSource } = await import('./androidDeviceSource.js');
  const src = createAndroidDeviceSource({ get: async () => [] });

  const granted = await src.requestPermission({ usb_vendor: '10c4', usb_product: 'ea60' });
  assert.equal(granted, true);
  assert.equal(calls.length, 1);
  assert.equal(calls[0].vid, 0x10c4);
  assert.equal(calls[0].pid, 0xea60);
});

after(() => {
  delete globalThis.GraywolfWebInterface;
  delete globalThis.__usbResult;
  delete globalThis.__usbCallbacks;
});
