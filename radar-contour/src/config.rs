use clap::Parser;

#[derive(Debug, Clone, Copy)]
pub struct BBox {
    pub west: f64,
    pub south: f64,
    pub east: f64,
    pub north: f64,
}

#[derive(Debug, Clone, Parser)]
#[command(about = "Level II reflectivity -> vector isoband PMTiles")]
pub struct Config {
    /// R2 bucket name
    #[arg(long, env = "RADAR_R2_BUCKET")]
    pub r2_bucket: Option<String>,
    /// R2 S3-compatible endpoint URL
    #[arg(long, env = "RADAR_R2_ENDPOINT")]
    pub r2_endpoint: Option<String>,
    /// Object-key prefix under the bucket
    #[arg(long, env = "RADAR_R2_PREFIX", default_value = "radar")]
    pub r2_prefix: String,
    #[arg(long, default_value_t = 3)]
    pub min_zoom: u8,
    #[arg(long, default_value_t = 10)]
    pub max_zoom: u8,
    #[arg(long, default_value_t = 1.0)]
    pub gaussian_sigma: f64,
    #[arg(long, default_value_t = 2)]
    pub chaikin_iterations: u8,
    /// Composite grid cell size in degrees (~0.0025deg ~= 250 m at mid-lat).
    #[arg(long, default_value_t = 0.0025)]
    pub grid_deg: f64,

    #[clap(skip = Config::default_bbox())]
    pub bbox: BBox,
    #[clap(skip = Config::default_thresholds())]
    pub dbz_thresholds: Vec<f64>,
}

impl Config {
    fn default_bbox() -> BBox {
        // CONUS, generous margins.
        BBox { west: -127.0, south: 20.0, east: -65.0, north: 51.0 }
    }
    fn default_thresholds() -> Vec<f64> {
        // NWS reflectivity breakpoints, 5..=75 by 5.
        (1..=15).map(|i| (i * 5) as f64).collect()
    }
    pub fn conus_defaults() -> Self {
        Self {
            r2_bucket: None,
            r2_endpoint: None,
            r2_prefix: "radar".into(),
            min_zoom: 3,
            max_zoom: 10,
            gaussian_sigma: 1.0,
            chaikin_iterations: 2,
            grid_deg: 0.0025,
            bbox: Self::default_bbox(),
            dbz_thresholds: Self::default_thresholds(),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn defaults_cover_conus_z3_z10() {
        let c = Config::conus_defaults();
        assert_eq!(c.min_zoom, 3);
        assert_eq!(c.max_zoom, 10);
        assert!(c.bbox.west < c.bbox.east);
        assert!(c.bbox.south < c.bbox.north);
        assert!(c.gaussian_sigma > 0.0);
        assert!(c.chaikin_iterations >= 1);
        assert!(c.grid_deg > 0.0 && c.grid_deg < 0.1);
        assert_eq!(c.dbz_thresholds.first().copied(), Some(5.0));
    }
}
