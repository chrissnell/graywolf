// graywolf/web/src/lib/map/sources/osm-raster.js
//
// Public OSM raster tiles wrapped as a MapLibre style. Used as the
// default basemap and as a fallback when the user hasn't registered
// for private maps. Tiles come straight from the OSM public servers,
// the same URLs Leaflet was hitting; no API key required.

export function osmRasterStyle() {
  return {
    version: 8,
    sources: {
      osm: {
        type: 'raster',
        tiles: [
          'https://a.tile.openstreetmap.org/{z}/{x}/{y}.png',
          'https://b.tile.openstreetmap.org/{z}/{x}/{y}.png',
          'https://c.tile.openstreetmap.org/{z}/{x}/{y}.png',
        ],
        tileSize: 256,
        maxzoom: 19,
        attribution:
          '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors',
      },
    },
    layers: [{ id: 'osm', type: 'raster', source: 'osm' }],
  };
}
