use crate::config::BBox;
use crate::contour::Band;
use crate::mercator::{tile_bounds_merc, tile_range};
use geo::{BooleanOps, Simplify};
use geo_types::{Coord, LineString, MultiPolygon, Polygon};

#[derive(Debug, Clone)]
pub struct TileFeature { pub dbz: f64, pub geom: MultiPolygon<f64> }

#[derive(Debug, Clone)]
pub struct PyramidTile { pub z: u8, pub x: u32, pub y: u32, pub features: Vec<TileFeature> }

#[derive(Debug, Clone, Copy)]
pub struct Aabb { pub minx: f64, pub miny: f64, pub maxx: f64, pub maxy: f64 }

pub fn aabb_overlap(a: &Aabb, b: &Aabb) -> bool {
    a.minx <= b.maxx && a.maxx >= b.minx && a.miny <= b.maxy && a.maxy >= b.miny
}

pub fn band_aabb(b: &Band) -> Aabb {
    let (mut minx, mut miny, mut maxx, mut maxy) = (f64::MAX, f64::MAX, f64::MIN, f64::MIN);
    for poly in &b.geom.0 {
        for c in poly.exterior().0.iter() {
            minx = minx.min(c.x); miny = miny.min(c.y);
            maxx = maxx.max(c.x); maxy = maxy.max(c.y);
        }
    }
    Aabb { minx, miny, maxx, maxy }
}

fn tile_clip_lonlat(z: u8, x: u32, y: u32, buffer_frac: f64) -> (Polygon<f64>, Aabb) {
    let b = tile_bounds_merc(z, x, y);
    let size = b.east - b.west;
    let pad = size * buffer_frac;
    let inv = |mx: f64, my: f64| {
        let lon = (mx / 6_378_137.0).to_degrees();
        let lat = (2.0 * (my / 6_378_137.0).exp().atan() - std::f64::consts::FRAC_PI_2).to_degrees();
        (lon, lat)
    };
    let (w, n) = inv(b.west - pad, b.north + pad);
    let (e, s) = inv(b.east + pad, b.south - pad);
    let poly = Polygon::new(LineString(vec![
        Coord{x:w,y:s}, Coord{x:e,y:s}, Coord{x:e,y:n}, Coord{x:w,y:n}, Coord{x:w,y:s},
    ]), vec![]);
    (poly, Aabb { minx: w, miny: s, maxx: e, maxy: n })
}

/// Per-zoom simplification tolerance (degrees). Low zooms simplify hard; high
/// zooms keep detail. ~ half a tile / EXTENT, clamped.
fn simplify_tol_deg(z: u8) -> f64 {
    let deg_per_tile = 360.0 / 2f64.powi(z as i32);
    (deg_per_tile / 4096.0 * 2.0).clamp(1e-6, 0.05)
}

pub fn build_pyramid(bands: &[Band], bbox: &BBox, min_zoom: u8, max_zoom: u8) -> Vec<PyramidTile> {
    // Precompute each band's AABB once (review #3: cheap reject before ∩).
    let band_boxes: Vec<Aabb> = bands.iter().map(band_aabb).collect();
    let mut tiles = Vec::new();

    for z in min_zoom..=max_zoom {
        let tol = simplify_tol_deg(z);
        // Simplify every band once per zoom, not per tile.
        let simplified: Vec<Band> = bands.iter().map(|b| Band {
            dbz: b.dbz,
            geom: b.geom.simplify(tol),
        }).collect();

        let (xmin, ymin, xmax, ymax) = tile_range(z, bbox);
        for x in xmin..=xmax {
            for y in ymin..=ymax {
                let (clip_poly, clip_box) = tile_clip_lonlat(z, x, y, 0.05);
                let clip = MultiPolygon(vec![clip_poly]);
                let mut features = Vec::new();
                for (bi, b) in simplified.iter().enumerate() {
                    if !aabb_overlap(&band_boxes[bi], &clip_box) { continue; } // pre-filter
                    let clipped = b.geom.intersection(&clip);
                    if !clipped.0.is_empty() {
                        features.push(TileFeature { dbz: b.dbz, geom: clipped });
                    }
                }
                if !features.is_empty() {
                    tiles.push(PyramidTile { z, x, y, features });
                }
            }
        }
    }
    tiles
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::contour::Band;
    use geo_types::{Coord, LineString, MultiPolygon, Polygon};

    fn square_band(dbz: f64, x0: f64, y0: f64, x1: f64, y1: f64) -> Band {
        let ring = LineString(vec![
            Coord{x:x0,y:y0}, Coord{x:x1,y:y0}, Coord{x:x1,y:y1},
            Coord{x:x0,y:y1}, Coord{x:x0,y:y0},
        ]);
        Band { dbz, geom: MultiPolygon(vec![Polygon::new(ring, vec![])]) }
    }

    #[test]
    fn clips_band_to_tile_bounds() {
        let bands = vec![square_band(20.0, -100.0, 30.0, -90.0, 40.0)];
        let bbox = crate::config::BBox { west:-100.0, south:30.0, east:-90.0, north:40.0 };
        let tiles = build_pyramid(&bands, &bbox, 5, 5);
        assert!(!tiles.is_empty());
        assert!(tiles.iter().all(|t| !t.features.is_empty()));
    }

    #[test]
    fn aabb_prefilter_rejects_non_overlapping_pairs() {
        // Band far to the west; a tile-box far to the east must early-reject.
        let band = square_band(20.0, -120.0, 30.0, -119.0, 31.0);
        let bb = band_aabb(&band);
        let east = Aabb { minx:-80.0, miny:30.0, maxx:-79.0, maxy:31.0 };
        assert!(!aabb_overlap(&bb, &east));
    }
}
