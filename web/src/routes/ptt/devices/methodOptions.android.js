// web/src/routes/ptt/devices/methodOptions.android.js
//
// Android PTT methods, in display order for DialogChangeMethod. Each
// entry's wire fragment is the body fragment POSTed to /api/ptt; the
// PttConfig schema for method='android' carries ptt_method (1..4) per
// invariant 9c. deviceClass is consumed by androidDeviceSource.list()
// to filter the device picker.

export const ANDROID_METHODS = [
  { wire: { method: 'none' },
    label: 'Off — no PTT',
    meta: 'Graywolf does not key the radio.' },
  { wire: { method: 'android', ptt_method: 1 },
    label: 'Digirig (CP2102N RTS)',
    meta: 'USB-serial RTS keys the radio. Most common option.',
    deviceClass: 'cp2102n' },
  { wire: { method: 'android', ptt_method: 3 },
    label: 'AIOC (CDC-ACM DTR)',
    meta: 'For AIOC firmware ≥ 1.2.0. DTR=1 / RTS=0.',
    deviceClass: 'cdc-acm' },
  { wire: { method: 'android', ptt_method: 2 },
    label: 'CM108 HID GPIO',
    meta: 'Generic CM108-class adapters with GPIO 3 wired externally to PTT. Not for Digirig or AIOC.',
    deviceClass: 'hid-cm108' },
  { wire: { method: 'android', ptt_method: 4 },
    label: 'VOX (no keying)',
    meta: 'Radio detects audio and keys itself. No USB device required.' },
  { wire: { method: 'android', ptt_method: 5 },
    label: 'Digirig Lite (tone PTT)',
    meta: 'No PTT wire — a tone on the right channel keys the radio while the packet plays on the left. Requires the Digirig Lite as the USB audio output.' },
];
