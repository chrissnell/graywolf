// Static bounding boxes for the 50 US states + DC. Keys match the slugs
// in state-list.js exactly. Values are [[swLat, swLon], [neLat, neLon]]
// in decimal degrees.
//
// Used in P2-T10 by the federated tile protocol to dispatch each tile
// request to the appropriate per-state PMTiles archive based on the
// tile's geographic bbox.
//
// Bounds are accurate to roughly +/- 0.5 deg, which is well within
// tile-level dispatch tolerance. Alaska's box stops at -179.15 (the
// Aleutian tail past the antimeridian is excluded -- ~5% of the state's
// area, almost entirely uninhabited water).

export const STATE_BOUNDS = {
  alabama:                [[30.137, -88.473], [35.008, -84.889]],
  alaska:                 [[51.21,  -179.15], [71.54,  -129.97]],
  arizona:                [[31.332, -114.819], [37.004, -109.045]],
  arkansas:               [[33.004, -94.617], [36.500, -89.644]],
  california:             [[32.534, -124.482], [42.009, -114.131]],
  colorado:               [[36.993, -109.060], [41.003, -102.041]],
  connecticut:            [[40.987, -73.728], [42.050, -71.787]],
  delaware:               [[38.451, -75.789], [39.840, -75.048]],
  'district-of-columbia': [[38.791, -77.119], [38.996, -76.910]],
  florida:                [[24.396, -87.635], [31.001, -79.974]],
  georgia:                [[30.357, -85.605], [35.001, -80.840]],
  hawaii:                 [[18.91,  -178.33], [28.40,  -154.81]],
  idaho:                  [[41.988, -117.243], [49.001, -111.043]],
  illinois:               [[36.970, -91.514], [42.508, -87.020]],
  indiana:                [[37.772, -88.098], [41.761, -84.785]],
  iowa:                   [[40.375, -96.640], [43.501, -90.140]],
  kansas:                 [[36.993, -102.052], [40.003, -94.589]],
  kentucky:               [[36.498, -89.571], [39.147, -81.965]],
  louisiana:              [[28.928, -94.043], [33.020, -88.817]],
  maine:                  [[42.977, -71.084], [47.460, -66.949]],
  maryland:               [[37.886, -79.488], [39.723, -75.048]],
  massachusetts:          [[41.187, -73.508], [42.886, -69.928]],
  michigan:               [[41.696, -90.418], [48.306, -82.122]],
  minnesota:              [[43.499, -97.239], [49.385, -89.491]],
  mississippi:            [[30.174, -91.655], [34.996, -88.097]],
  missouri:               [[35.996, -95.774], [40.613, -89.099]],
  montana:                [[44.358, -116.050], [49.001, -104.039]],
  nebraska:               [[39.999, -104.054], [43.001, -95.308]],
  nevada:                 [[35.001, -120.005], [42.000, -114.038]],
  'new-hampshire':        [[42.697, -72.557], [45.305, -70.610]],
  'new-jersey':           [[38.928, -75.560], [41.357, -73.894]],
  'new-mexico':           [[31.332, -109.050], [37.000, -103.001]],
  'new-york':             [[40.496, -79.763], [45.016, -71.857]],
  'north-carolina':       [[33.842, -84.322], [36.588, -75.460]],
  'north-dakota':         [[45.935, -104.049], [49.001, -96.554]],
  ohio:                   [[38.403, -84.820], [42.327, -80.519]],
  oklahoma:               [[33.616, -103.003], [37.002, -94.431]],
  oregon:                 [[41.992, -124.566], [46.292, -116.464]],
  pennsylvania:           [[39.720, -80.519], [42.269, -74.690]],
  'rhode-island':         [[41.146, -71.862], [42.018, -71.120]],
  'south-carolina':       [[32.034, -83.354], [35.215, -78.541]],
  'south-dakota':         [[42.479, -104.058], [45.945, -96.436]],
  tennessee:              [[34.983, -90.310], [36.679, -81.647]],
  texas:                  [[25.837, -106.646], [36.501, -93.508]],
  utah:                   [[36.998, -114.052], [42.001, -109.041]],
  vermont:                [[42.727, -73.438], [45.017, -71.465]],
  virginia:               [[36.541, -83.675], [39.466, -75.242]],
  washington:             [[45.544, -124.733], [49.002, -116.916]],
  'west-virginia':        [[37.202, -82.644], [40.638, -77.719]],
  wisconsin:              [[42.492, -92.889], [47.080, -86.250]],
  wyoming:                [[40.995, -111.057], [45.006, -104.052]],
};
