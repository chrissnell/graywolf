use crate::config::BBox;
use crate::field::FieldGrid;
use geo_types::{Coord, LineString, MultiPolygon, Polygon};

const R: f64 = 6_378_137.0;
pub const HALF: f64 = std::f64::consts::PI * R;

pub fn lonlat_to_merc(lon: f64, lat: f64) -> (f64, f64) {
    let x = R * lon.to_radians();
    let y = R * ((std::f64::consts::FRAC_PI_4 + lat.to_radians() / 2.0).tan()).ln();
    (x, y)
}

#[derive(Debug, Clone, Copy)]
pub struct MercBox { pub west: f64, pub south: f64, pub east: f64, pub north: f64 }

pub fn tile_bounds_merc(z: u8, x: u32, y: u32) -> MercBox {
    let n = 2f64.powi(z as i32);
    let size = (2.0 * HALF) / n;
    let west = -HALF + x as f64 * size;
    let east = west + size;
    let north = HALF - y as f64 * size;
    let south = north - size;
    MercBox { west, south, east, north }
}

pub fn tile_range(z: u8, bbox: &BBox) -> (u32, u32, u32, u32) {
    let (wx, ny) = lonlat_to_merc(bbox.west, bbox.north);
    let (ex, sy) = lonlat_to_merc(bbox.east, bbox.south);
    let n = 2f64.powi(z as i32);
    let size = (2.0 * HALF) / n;
    let xmin = (((wx + HALF) / size).floor() as i64).clamp(0, n as i64 - 1) as u32;
    let xmax = (((ex + HALF) / size).floor() as i64).clamp(0, n as i64 - 1) as u32;
    let ymin = (((HALF - ny) / size).floor() as i64).clamp(0, n as i64 - 1) as u32;
    let ymax = (((HALF - sy) / size).floor() as i64).clamp(0, n as i64 - 1) as u32;
    (xmin, ymin, xmax, ymax)
}

/// Map a grid-index coord (col=x, row=y, possibly fractional) to lon/lat.
pub fn grid_to_lonlat(g: &FieldGrid, x: f64, y: f64) -> (f64, f64) {
    (g.lon0 + x * g.dlon, g.lat0 + y * g.dlat)
}

/// Lift a grid-space MultiPolygon to lon/lat.
pub fn lift_multipolygon(g: &FieldGrid, mp: &MultiPolygon<f64>) -> MultiPolygon<f64> {
    let map_ring = |ls: &LineString<f64>| LineString(
        ls.0.iter().map(|c| {
            let (lon, lat) = grid_to_lonlat(g, c.x, c.y);
            Coord { x: lon, y: lat }
        }).collect()
    );
    MultiPolygon(mp.0.iter().map(|poly| {
        Polygon::new(map_ring(poly.exterior()), poly.interiors().iter().map(map_ring).collect())
    }).collect())
}

#[cfg(test)]
mod tests {
    use super::*;
    use approx::assert_relative_eq;

    #[test]
    fn lonlat_to_mercator_origin_and_axes() {
        let (x, y) = lonlat_to_merc(0.0, 0.0);
        assert_relative_eq!(x, 0.0, epsilon = 1e-6);
        assert_relative_eq!(y, 0.0, epsilon = 1e-6);
        let (xe, _) = lonlat_to_merc(10.0, 0.0);
        let (_, yn) = lonlat_to_merc(0.0, 10.0);
        assert!(xe > 0.0 && yn > 0.0);
    }

    #[test]
    fn tile_bounds_cover_world_at_z0() {
        let b = tile_bounds_merc(0, 0, 0);
        assert!(b.west < 0.0 && b.east > 0.0 && b.south < 0.0 && b.north > 0.0);
    }
}
