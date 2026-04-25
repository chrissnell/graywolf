// graywolf/web/src/lib/map/sources/graywolf-vector.js
//
// Private GW vector tiles via maps.nw5w.com. Returns the URL of the
// shared americana-roboto style.json. The style itself is public; the
// underlying tiles and tilejson are gated behind a bearer token,
// which the map shell applies via transformRequest.

export function graywolfVectorStyle() {
  return 'https://maps.nw5w.com/style/americana-roboto/style.json';
}
