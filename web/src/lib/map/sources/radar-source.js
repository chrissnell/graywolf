// Canonical NWS reflectivity ramp. Each key is the lower-bound dBZ of a
// filled isoband (matches the `dbz` attribute the generator writes per
// polygon). Coloring is client-side so the same tiles recolor without
// regenerating.
export const DBZ_THRESHOLDS = [5, 10, 15, 20, 25, 30, 35, 40, 45, 50, 55, 60, 65, 70, 75];

export const DBZ_COLORS = {
  5: '#04e9e7', 10: '#019ff4', 15: '#0300f4', 20: '#02fd02', 25: '#01c501',
  30: '#008e00', 35: '#fdf802', 40: '#e5bc00', 45: '#fd9500', 50: '#fd0000',
  55: '#d40000', 60: '#bc0000', 65: '#f800fd', 70: '#9854c6', 75: '#fdfdfd',
};

export function fillColorExpression() {
  const expr = ['step', ['get', 'dbz'], 'rgba(0,0,0,0)'];
  for (const t of DBZ_THRESHOLDS) expr.push(t, DBZ_COLORS[t]);
  return expr;
}

export const RADAR_SOURCE_ID = 'radar';
export const RADAR_LAYER_ID = 'radar-fill';
export const RADAR_LATEST_URL = 'https://maps.nw5w.com/radar/latest.json';

export function radarTileUrl(ts) {
  return `https://maps.nw5w.com/radar/${ts}/{z}/{x}/{y}.mvt`;
}
