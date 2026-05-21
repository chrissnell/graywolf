// web/src/routes/ptt/devices/androidDeviceSource.js
//
// Android adapter for DialogChangeDevice. Lists devices from
// /api/ptt/available (Phase 5 wires the Android server-side branch)
// and routes permission requests through the JS bridge exposed by
// the Android WebView (GraywolfWebInterface.requestUsbPermission).

export function createAndroidDeviceSource(api) {
  return {
    async list(method) {
      const cls = method?.deviceClass;
      if (!cls) return [];
      const all = await api.get('/ptt/available') || [];
      const expectType = ({
        'cp2102n':   'usb-cp2102n',
        'cdc-acm':   'usb-cdc-acm',
        'hid-cm108': 'usb-hid',
      })[cls];
      if (!expectType) return [];
      return all
        .filter(d => d.type === expectType)
        .map(d => ({
          ...d,
          // Android USB devices don't have stable POSIX paths (the bus
          // path /dev/bus/usb/NNN/MMM rotates on reconnect). Synthesize
          // a usb:VID:PID identifier so DevicePicker's selectedPath
          // comparison works and the saved device_path round-trips on
          // re-open.
          path: d.path || `usb:${d.usb_vendor}:${d.usb_product}`,
        }));
    },
    async requestPermission(device) {
      const bridge = globalThis.GraywolfWebInterface;
      if (!bridge?.requestUsbPermission) return false;
      // Singleton callback-dispatcher pattern shared across Android JS-bridge
      // flows. The Kotlin side calls evaluateJavascript("__usbResult(id, granted)")
      // when the system USB permission dialog returns; we route to the per-call
      // promise via __usbCallbacks[id]. Idempotent install — Kiss.svelte uses
      // the same shape with __btResult / __btCallbacks. Removing or replacing
      // either one breaks the other.
      if (!globalThis.__usbResult) {
        globalThis.__usbResult = (id, granted) => {
          const cb = globalThis.__usbCallbacks?.[id];
          if (cb) cb(granted);
          delete globalThis.__usbCallbacks?.[id];
        };
        globalThis.__usbCallbacks = {};
      }
      return new Promise((resolve) => {
        const callbackId = 'cb' + Math.random().toString(36).slice(2);
        globalThis.__usbCallbacks[callbackId] = (granted) => resolve(!!granted);
        try {
          bridge.requestUsbPermission(
            parseInt(device.usb_vendor, 16),
            parseInt(device.usb_product, 16),
            callbackId,
          );
        } catch {
          delete globalThis.__usbCallbacks[callbackId];
          resolve(false);
        }
      });
    },
  };
}
