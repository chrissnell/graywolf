use anyhow::Result;
use crate::config::BBox;

pub mod level2;
pub mod mrms;
pub mod n0q;

/// A dBZ field on a regular lon/lat grid, row-major from the NW corner.
/// `dlat` is typically negative (rows march south). Missing data is encoded
/// as f64::NAN.
#[derive(Debug, Clone)]
pub struct FieldGrid {
    pub cols: usize,
    pub rows: usize,
    pub lon0: f64,
    pub lat0: f64,
    pub dlon: f64,
    pub dlat: f64,
    pub values: Vec<f64>, // len == cols*rows
}

impl FieldGrid {
    pub fn at(&self, col: usize, row: usize) -> f64 {
        self.values[row * self.cols + col]
    }
    /// Geographic center of a cell.
    pub fn cell_center(&self, col: usize, row: usize) -> (f64, f64) {
        let lon = self.lon0 + (col as f64 + 0.5) * self.dlon;
        let lat = self.lat0 + (row as f64 + 0.5) * self.dlat;
        (lon, lat)
    }
}

/// Target lattice for compositing polar sources. North-up: row 0 is the
/// north edge, dlat negative. `lon0`/`lat0` are the NW node (corner) coords.
#[derive(Debug, Clone, Copy)]
pub struct GridSpec {
    pub cols: usize,
    pub rows: usize,
    pub lon0: f64,
    pub lat0: f64,
    pub dlon: f64, // = grid_deg
    pub dlat: f64, // = -grid_deg
}

impl GridSpec {
    pub fn from_bbox(bbox: &BBox, grid_deg: f64) -> Self {
        let cols = (((bbox.east - bbox.west) / grid_deg).round() as usize).max(1);
        let rows = (((bbox.north - bbox.south) / grid_deg).round() as usize).max(1);
        GridSpec {
            cols, rows,
            lon0: bbox.west,
            lat0: bbox.north,
            dlon: grid_deg,
            dlat: -grid_deg,
        }
    }
    /// Nearest (col, row) for a lon/lat, or None if outside the lattice.
    pub fn cell_index(&self, lon: f64, lat: f64) -> Option<(usize, usize)> {
        let fx = (lon - self.lon0) / self.dlon;
        let fy = (lat - self.lat0) / self.dlat; // dlat negative -> grows south
        if fx < 0.0 || fy < 0.0 { return None; }
        let (cx, cy) = (fx.floor() as usize, fy.floor() as usize);
        if cx >= self.cols || cy >= self.rows { return None; }
        Some((cx, cy))
    }
    /// An all-NaN FieldGrid sized to this spec, ready for max-compositing.
    pub fn empty_field(&self) -> FieldGrid {
        FieldGrid {
            cols: self.cols, rows: self.rows,
            lon0: self.lon0, lat0: self.lat0,
            dlon: self.dlon, dlat: self.dlat,
            values: vec![f64::NAN; self.cols * self.rows],
        }
    }
}

/// Swappable field source. v1 ships Level II (per-site super-res) as the
/// default; MRMS + N0Q remain as fallback implementors behind this trait.
/// Async because every implementor is backed by anonymous S3/HTTP reads.
#[async_trait::async_trait]
pub trait ReflectivityField {
    /// Identifier of the latest available frame (e.g. a cadence-rounded
    /// RFC3339 timestamp). Used for change detection.
    async fn latest_frame_id(&self) -> Result<String>;
    /// Fetch + decode + (for Level II) composite the field for a frame id.
    async fn fetch(&self, frame_id: &str) -> Result<FieldGrid>;
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn grid_indexes_row_major_and_reports_geo() {
        // 2 cols x 2 rows, origin at (lon0,lat0), 1deg cells.
        let g = FieldGrid {
            cols: 2, rows: 2,
            lon0: -100.0, lat0: 40.0, dlon: 1.0, dlat: -1.0,
            values: vec![0.0, 1.0, 2.0, 3.0],
        };
        assert_eq!(g.at(0, 0), 0.0);
        assert_eq!(g.at(1, 0), 1.0); // col 1, row 0
        assert_eq!(g.at(0, 1), 2.0); // col 0, row 1
        let (lon, lat) = g.cell_center(1, 1);
        assert!((lon - (-98.5)).abs() < 1e-9);
        assert!((lat - 38.5).abs() < 1e-9);
    }

    #[test]
    fn gridspec_maps_lonlat_to_nearest_cell() {
        let spec = GridSpec::from_bbox(
            &crate::config::BBox { west: -100.0, south: 30.0, east: -90.0, north: 40.0 }, 1.0);
        assert_eq!(spec.cols, 10);
        assert_eq!(spec.rows, 10);
        // A point just inside the NW corner lands at (col 0, row 0).
        assert_eq!(spec.cell_index(-99.9, 39.9), Some((0, 0)));
        // Out of bounds -> None.
        assert_eq!(spec.cell_index(-80.0, 39.0), None);
    }
}
