// Desktop PTT methods, in display order for DialogChangeMethod.
// `wire` is the body fragment POSTed to /api/ptt; `label` and `meta`
// are operator-facing. `deviceClass` is unused on desktop (DialogChangeDevice
// filters by method directly in desktopDeviceSource).

export const DESKTOP_METHODS = [
  { wire: { method: 'none' },
    label: 'Off — no PTT',
    meta: 'Graywolf does not key the radio.' },
  { wire: { method: 'serial_rts' },
    label: 'Serial RTS',
    meta: 'USB-serial RTS line keys the radio. Use for FTDI / CP210x / CH340 cables.' },
  { wire: { method: 'serial_dtr' },
    label: 'Serial DTR',
    meta: 'USB-serial DTR line keys the radio. Less common than RTS.' },
  { wire: { method: 'gpio' },
    label: 'GPIO',
    meta: 'Linux gpiochip line keys the radio. Use on Raspberry Pi-style SBCs.' },
  { wire: { method: 'cm108' },
    label: 'CM108 HID GPIO',
    meta: 'CM108-family USB audio adapter with HID-controlled GPIO (Digirig, AIOC).' },
  { wire: { method: 'rigctld' },
    label: 'Hamlib rigctld (CAT)',
    meta: 'Key the radio over CAT through a running rigctld daemon.' },
];
