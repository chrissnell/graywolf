// Parsing for the manual lat/lon entry fields in the fixed-point dialog
// (graywolf#417). Operators paste or type coordinates copied from other
// tools, so we accept decimal degrees with an optional leading sign and an
// optional hemisphere letter (N/S/E/W), e.g. "37.7749", "-122.4194",
// "37.7749 N", "122.4194W". The hemisphere letter, when present, wins over a
// sign and must match the axis (N/S for latitude, E/W for longitude).

const RANGE = {
  lat: { max: 90, neg: 'S', pos: 'N', label: 'Latitude' },
  lon: { max: 180, neg: 'W', pos: 'E', label: 'Longitude' },
};

// parseCoordinate turns a user-entered string into a signed decimal-degree
// number for the given axis ('lat' | 'lon'). Returns { value } on success or
// { error } with a human-readable message suitable for inline display.
export function parseCoordinate(text, axis) {
  const spec = RANGE[axis];
  const raw = String(text ?? '').trim();
  if (!raw) return { error: `${spec.label} is required` };

  // Pull an optional trailing/leading hemisphere letter off the number.
  const m = raw.match(/^([nsew])?\s*([+-]?\d*\.?\d+)\s*([nsew])?$/i);
  if (!m) return { error: `${spec.label} is not a valid number` };

  const hemi = (m[1] || m[3] || '').toUpperCase();
  let value = parseFloat(m[2]);
  if (!Number.isFinite(value)) return { error: `${spec.label} is not a valid number` };

  if (hemi) {
    if (hemi !== spec.neg && hemi !== spec.pos) {
      return { error: `Use ${spec.pos} or ${spec.neg} for ${spec.label.toLowerCase()}` };
    }
    // A sign and a contradicting hemisphere letter is ambiguous; reject it
    // rather than silently picking one.
    if (value < 0) return { error: `Don't combine a minus sign with ${hemi}` };
    if (hemi === spec.neg) value = -value;
  }

  if (Math.abs(value) > spec.max) {
    return { error: `${spec.label} must be between -${spec.max} and ${spec.max}` };
  }
  return { value };
}
