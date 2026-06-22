// Desktop PTT methods, in display order for DialogChangeMethod.
// `wire` is the body fragment POSTed to /api/ptt; `label` and `meta`
// are operator-facing. `deviceClass` is unused on desktop (DialogChangeDevice
// filters by method directly in desktopDeviceSource).

export const DESKTOP_METHODS = [
  { wire: { method: 'none' },
    label: 'Off — no PTT',
    meta: 'Graywolf does not key the radio.' },
  { wire: { method: 'vox' },
    label: 'VOX (voice-operated)',
    meta: 'No PTT wire — the radio keys on audio. Graywolf sends a 500 ms tone before each packet so the VOX circuit engages before data starts.' },
  { wire: { method: 'digirig_tone' },
    label: 'Digirig Lite Tone PTT',
    meta: 'No PTT wire — the Digirig Lite keys the radio from a tone on the right channel while the packet audio plays on the left. Set this channel\'s output to the Left channel of a stereo Digirig Lite device.' },
  { wire: { method: 'serial_rts' },
    label: 'Serial RTS',
    meta: 'USB-serial RTS line keys the radio. The right choice for the Digirig Mobile, and works with FTDI / CP210x / CH340 cables.' },
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
