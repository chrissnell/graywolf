// Position-source and SmartBeaconing flags a beacon should be saved
// with, derived from its type and the form's chosen position source.
//
// A tracker is a GPS-driven mobile beacon: the backend builder always
// reads its position (and the CSE/SPD course-speed extension) from the
// live GPS fix, and the scheduler only engages SmartBeaconing when the
// beacon type is `tracker` AND the per-beacon `smart_beacon` flag is set
// (alongside the global SmartBeacon master switch). So a tracker always
// forces use_gps and opts into smart_beacon, regardless of the form's
// pos_source radio. Every other type leaves smart_beacon off and honors
// the operator's GPS/fixed choice.
//
// Pure helper so the Beacons page and tests share one source of truth
// for the rule the SmartBeaconing fix hinges on.
export function trackerBeaconFlags(type, posSource) {
  const isTracker = type === 'tracker';
  return {
    use_gps: isTracker || posSource === 'gps',
    smart_beacon: isTracker,
  };
}
