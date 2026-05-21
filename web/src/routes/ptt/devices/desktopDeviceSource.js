// web/src/routes/ptt/devices/desktopDeviceSource.js
//
// Adapter that feeds DialogChangeDevice on desktop. Wraps the existing
// `/api/ptt/available` endpoint and filters by the method's expected
// device type so the dialog only shows relevant rows.

const METHOD_TO_TYPE = {
  serial_rts: 'serial',
  serial_dtr: 'serial',
  gpio: 'gpio',
  cm108: 'cm108',
};

export function createDesktopDeviceSource(api) {
  return {
    async list(method) {
      const wireMethod = method?.wire?.method;
      const wantType = METHOD_TO_TYPE[wireMethod];
      if (!wantType) return [];
      const all = await api.get('/ptt/available') || [];
      return all.filter(d => d.type === wantType);
    },
    // No permission flow on desktop — devices are POSIX paths with
    // standard filesystem permissions. requestPermission stays undefined
    // so DialogChangeDevice hides the CTA.
  };
}
