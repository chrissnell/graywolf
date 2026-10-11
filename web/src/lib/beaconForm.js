export const BEACON_TYPE_OPTIONS = Object.freeze([
  {
    value: 'position',
    label: 'Position',
    description: 'a beacon for a station.',
  },
  {
    value: 'object',
    label: 'Object',
    description: 'a named item such as a repeater, event site, or hospital.',
  },
  {
    value: 'tracker',
    label: 'Tracker',
    description: 'a GPS-driven mobile beacon that uses SmartBeaconing to adapt its rate to speed and turns.',
  },
  {
    value: 'custom',
    label: 'Custom',
    description: 'a raw APRS information field, optionally followed by a static or command-generated comment.',
  },
]);

export function beaconTypeUsesPosition(type) {
  return type !== 'custom';
}

export function beaconExtraFields(row = {}) {
  return {
    custom_info: typeof row.custom_info === 'string' ? row.custom_info : '',
    comment_cmd: typeof row.comment_cmd === 'string' ? row.comment_cmd : '',
  };
}

export function validateCustomBeacon(form) {
  if (form?.type !== 'custom') return '';
  if (typeof form.custom_info !== 'string' || form.custom_info.trim() === '') {
    return 'Raw APRS information is required for a custom beacon';
  }
  return '';
}
