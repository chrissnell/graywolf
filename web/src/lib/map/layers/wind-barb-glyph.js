// Pure wind-barb glyph builder -- no DOM / MapLibre dependencies so it
// can be unit-tested under node and reused by the map layer. Produces the
// inner SVG markup for a standard meteorological wind barb.
//
// Convention (WMO): the staff points toward the direction the wind blows
// FROM; speed is encoded in knots as half barbs (5 kt), full barbs
// (10 kt) and pennants (50 kt). Calm is an open ring.

const KT_PER_MPH = 0.868976;

// Geometry in a local "north-up" frame: the staff points straight up and
// the whole glyph is rotated to the wind bearing by the caller's <g>.
const GAP = 11; // clear radius around the ~21px station icon
const STAFF = 30; // staff length
const STEP = 7; // spacing between successive ticks
const PENNANT_BASE = 7; // staff length consumed by one pennant
// Full-barb tick: out to the left and angled back toward the tip (~65°
// off the staff) so it reads as a barb, not a plus sign.
const FULL_DX = -11.8;
const FULL_DY = -5.5;
const HALF_DX = FULL_DX / 2;
const HALF_DY = FULL_DY / 2;

function n2(v) {
  return Number(v.toFixed(2));
}

// quantizeKnots rounds an mph reading to the 5-kt resolution a barb draws.
export function quantizeKnots(mph) {
  if (mph == null || !isFinite(mph)) return 0;
  return Math.round((mph * KT_PER_MPH) / 5) * 5;
}

// buildWindBarb returns the inner SVG markup (no <svg> wrapper) for a barb
// at the given sustained wind speed (mph) and direction (degrees true,
// the bearing the wind blows FROM). Returns '' when there's nothing
// meaningful to draw.
export function buildWindBarb(mph, dirDeg) {
  if (mph == null || dirDeg == null || !isFinite(mph)) return '';

  // Quantize to the nearest 5 kt -- the resolution a barb can express.
  const knots = quantizeKnots(mph);

  if (knots <= 0) {
    return `<circle cx="0" cy="0" r="6.5" class="wb-calm"/>`;
  }

  let rem = knots;
  const pennants = Math.floor(rem / 50);
  rem -= pennants * 50;
  const full = Math.floor(rem / 10);
  rem -= full * 10;
  const half = Math.floor(rem / 5);

  const yStart = -GAP;
  const yTip = -(GAP + STAFF);
  const parts = [`<g transform="rotate(${n2(dirDeg)})">`];
  parts.push(`<line x1="0" y1="${yStart}" x2="0" y2="${yTip}" class="wb-staff"/>`);

  // Walk from the tip back toward the station: pennants, then full barbs,
  // then a half barb.
  let y = yTip;

  for (let i = 0; i < pennants; i++) {
    parts.push(
      `<polygon points="0,${n2(y)} ${n2(FULL_DX)},${n2(y + FULL_DY)} 0,${n2(y + PENNANT_BASE)}" class="wb-pennant"/>`,
    );
    y += PENNANT_BASE;
  }
  if (pennants && (full || half)) y += 2;

  for (let i = 0; i < full; i++) {
    parts.push(
      `<line x1="0" y1="${n2(y)}" x2="${n2(FULL_DX)}" y2="${n2(y + FULL_DY)}" class="wb-barb"/>`,
    );
    y += STEP;
  }

  if (half) {
    // A lone half barb is inset one step from the tip so it can't be
    // mistaken for the staff tip -- standard plotting convention.
    if (!pennants && !full) y += STEP;
    parts.push(
      `<line x1="0" y1="${n2(y)}" x2="${n2(HALF_DX)}" y2="${n2(y + HALF_DY)}" class="wb-barb"/>`,
    );
  }

  parts.push('</g>');
  return parts.join('');
}
