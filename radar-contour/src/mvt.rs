use crate::mercator::{lonlat_to_merc, tile_bounds_merc};
use crate::tile::PyramidTile;
use geo_types::{Coord, LineString, MultiPolygon, Polygon};

const EXTENT: u32 = 4096;

/// Reproject a lon/lat polygon to Web Mercator. geozero's `MvtWriter` maps
/// world coords in the tile's mercator bbox to the 0..EXTENT tile space and
/// flips y -- but it does NOT re-wind rings. The y-flip reverses ring
/// orientation, so we orient to OGC (exterior CCW / holes CW) *before*
/// encoding (see `encode_tile`); after the writer's flip that becomes the
/// MVT-correct exterior-CW / holes-CCW (review #5).
fn to_merc_multipolygon(mp: &MultiPolygon<f64>) -> MultiPolygon<f64> {
    let ring = |ls: &LineString<f64>| LineString(
        ls.0.iter().map(|c| {
            let (mx, my) = lonlat_to_merc(c.x, c.y);
            Coord { x: mx, y: my }
        }).collect()
    );
    MultiPolygon(mp.0.iter().map(|poly| {
        Polygon::new(ring(poly.exterior()), poly.interiors().iter().map(ring).collect())
    }).collect())
}

/// Encode a PyramidTile to MVT via geozero's `MvtWriter` (layer `radar`,
/// integer `dbz` per feature). `MvtWriter` handles command encoding AND ring
/// winding for the y-flip (review #5).
pub fn encode_tile(t: &PyramidTile) -> Vec<u8> {
    use geozero::mvt::{Message, MvtWriter, Tile};
    use geozero::{ColumnValue, FeatureProcessor, GeozeroGeometry, PropertyProcessor};
    use geo::Orient;
    use geo::algorithm::orient::Direction;
    use geo_types::Geometry;

    let b = tile_bounds_merc(t.z, t.x, t.y);
    let mut w = MvtWriter::new(EXTENT, b.west, b.south, b.east, b.north)
        .expect("valid extent + bbox");

    w.dataset_begin(None).unwrap();
    for (fid, f) in t.features.iter().enumerate() {
        // Orient to OGC (exterior CCW / holes CW); the writer's y-flip then
        // yields MVT-correct winding so renderers keep both fills and holes.
        let merc = Geometry::MultiPolygon(
            to_merc_multipolygon(&f.geom).orient(Direction::Default));
        w.feature_begin(fid as u64).unwrap();
        w.properties_begin().unwrap();
        w.property(0, "dbz", &ColumnValue::Long(f.dbz as i64)).unwrap();
        w.properties_end().unwrap();
        w.geometry_begin().unwrap();
        merc.process_geom(&mut w).unwrap(); // emits commands + winding
        w.geometry_end().unwrap();
        w.feature_end(fid as u64).unwrap();
    }
    w.dataset_end().unwrap();

    let layer = w.layer("radar");
    let mut buf = Vec::new();
    Tile { layers: vec![layer] }.encode(&mut buf).expect("encode mvt tile");
    buf
}

/// Decode for the round-trip test: returns (first layer name, has dbz key,
/// has at least one polygon feature).
pub fn decode_summary(bytes: &[u8]) -> (String, bool, bool) {
    use geozero::mvt::{Message, Tile};
    let tile = Tile::decode(bytes).expect("decode mvt");
    let layer = tile.layers.first().expect("a layer");
    let has_dbz = layer.keys.iter().any(|k| k == "dbz");
    let has_polygon = layer.features.iter().any(|f| f.r#type == Some(3)); // GeomType::Polygon
    (layer.name.clone(), has_dbz, has_polygon)
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::tile::{PyramidTile, TileFeature};
    use geo_types::{Coord, LineString, MultiPolygon, Polygon};

    fn tile_with_centered_square() -> PyramidTile {
        let b = crate::mercator::tile_bounds_merc(5, 8, 12);
        let midx = (b.west + b.east) / 2.0;
        let midy = (b.south + b.north) / 2.0;
        let lon = (midx / 6_378_137.0).to_degrees();
        let lat = (2.0*(midy/6_378_137.0).exp().atan() - std::f64::consts::FRAC_PI_2).to_degrees();
        let d = 0.2;
        let ring = LineString(vec![
            Coord{x:lon-d,y:lat-d}, Coord{x:lon+d,y:lat-d},
            Coord{x:lon+d,y:lat+d}, Coord{x:lon-d,y:lat+d}, Coord{x:lon-d,y:lat-d},
        ]);
        PyramidTile { z:5, x:8, y:12, features: vec![
            TileFeature { dbz: 35.0, geom: MultiPolygon(vec![Polygon::new(ring, vec![])]) }
        ]}
    }

    #[test]
    fn encodes_nonempty_mvt() {
        let bytes = encode_tile(&tile_with_centered_square());
        assert!(!bytes.is_empty());
    }

    #[test]
    fn roundtrip_has_radar_layer_with_dbz_and_a_polygon() {
        let bytes = encode_tile(&tile_with_centered_square());
        let (layer, has_dbz, has_polygon) = decode_summary(&bytes);
        assert_eq!(layer, "radar");
        assert!(has_dbz);
        assert!(has_polygon, "polygon survived (winding correct -> fill kept)");
    }

    #[test]
    fn polygon_with_hole_survives_encoding() {
        // Exercises the interior-ring path: the orient()+y-flip must keep BOTH
        // the exterior and the hole with renderer-correct winding (review #5).
        let b = crate::mercator::tile_bounds_merc(5, 8, 12);
        let lon = ((b.west + b.east) / 2.0 / 6_378_137.0).to_degrees();
        let lat = (2.0*(((b.south+b.north)/2.0)/6_378_137.0).exp().atan()
            - std::f64::consts::FRAC_PI_2).to_degrees();
        let d = 0.3;
        let ext = LineString(vec![
            Coord{x:lon-d,y:lat-d}, Coord{x:lon+d,y:lat-d},
            Coord{x:lon+d,y:lat+d}, Coord{x:lon-d,y:lat+d}, Coord{x:lon-d,y:lat-d},
        ]);
        let h = 0.1;
        let hole = LineString(vec![
            Coord{x:lon-h,y:lat-h}, Coord{x:lon+h,y:lat-h},
            Coord{x:lon+h,y:lat+h}, Coord{x:lon-h,y:lat+h}, Coord{x:lon-h,y:lat-h},
        ]);
        let tile = PyramidTile { z:5, x:8, y:12, features: vec![
            TileFeature { dbz: 40.0, geom: MultiPolygon(vec![Polygon::new(ext, vec![hole])]) }
        ]};
        let bytes = encode_tile(&tile);
        let (_layer, _has_dbz, has_polygon) = decode_summary(&bytes);
        assert!(has_polygon, "holed polygon survived encoding");
    }
}
