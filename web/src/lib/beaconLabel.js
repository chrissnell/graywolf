// Human-readable label for a beacon row. Used by the Beacons page
// (list cards, toasts, delete confirmation) and the Dashboard page
// (per-channel "Beacon Now" buttons). One canonical helper so every
// surface displays the same identifier for a given row.
//
// Object beacons get their object_name as the label — that is what
// appears on aprs.fi and is what an operator scanning a list cares
// about. Every other beacon type renders the source callsign: the
// per-row override if set, otherwise the station callsign, otherwise
// the literal "(unset)" sentinel so callers don't have to special-case
// the empty string.
export function beaconLabel(row, stationCallsign = '') {
  if (row?.type === 'object' && row.object_name) return row.object_name;
  return row?.callsign || stationCallsign || '(unset)';
}
