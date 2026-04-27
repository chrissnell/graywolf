// Port of toMaidenhead from LiveMap.svelte:40-51 -- Maidenhead grid
// locator (4-character + 2 lowercase subsquare characters).

export function toMaidenhead(lat, lon) {
  lon += 180;
  lat += 90;
  return (
    String.fromCharCode(65 + Math.floor(lon / 20)) +
    String.fromCharCode(65 + Math.floor(lat / 10)) +
    Math.floor((lon % 20) / 2) +
    Math.floor(lat % 10) +
    String.fromCharCode(97 + Math.floor((lon % 2) * 12)) +
    String.fromCharCode(97 + Math.floor((lat % 1) * 24))
  );
}
