use crate::config::BBox;

#[derive(Debug, Clone, Copy)]
pub struct Site {
    pub id: &'static str,
    pub lat: f64,
    pub lon: f64,
    pub elev_m: f64,
}

/// WSR-88D site catalog (CONUS WSR-88D network). Source of truth: NWS/ROC
/// station list. Coordinates are tower lat/lon (deg) and elevation (m).
const SITES: &[Site] = &[
    Site { id: "KABR", lat: 45.4558, lon: -98.4131, elev_m: 397.0 },
    Site { id: "KABX", lat: 35.1497, lon: -106.8239, elev_m: 1789.0 },
    Site { id: "KAKQ", lat: 36.9840, lon: -77.0073, elev_m: 33.0 },
    Site { id: "KAMA", lat: 35.2333, lon: -101.7092, elev_m: 1093.0 },
    Site { id: "KAMX", lat: 25.6111, lon: -80.4128, elev_m: 4.0 },
    Site { id: "KAPX", lat: 44.9072, lon: -84.7197, elev_m: 446.0 },
    Site { id: "KARX", lat: 43.8228, lon: -91.1911, elev_m: 389.0 },
    Site { id: "KATX", lat: 48.1947, lon: -122.4956, elev_m: 151.0 },
    Site { id: "KBBX", lat: 39.4961, lon: -121.6317, elev_m: 53.0 },
    Site { id: "KBGM", lat: 42.1997, lon: -75.9847, elev_m: 490.0 },
    Site { id: "KBHX", lat: 40.4983, lon: -124.2919, elev_m: 732.0 },
    Site { id: "KBIS", lat: 46.7708, lon: -100.7606, elev_m: 505.0 },
    Site { id: "KBLX", lat: 45.8538, lon: -108.6068, elev_m: 1097.0 },
    Site { id: "KBMX", lat: 33.1722, lon: -86.7697, elev_m: 197.0 },
    Site { id: "KBOX", lat: 41.9558, lon: -71.1369, elev_m: 36.0 },
    Site { id: "KBRO", lat: 25.9161, lon: -97.4189, elev_m: 7.0 },
    Site { id: "KBUF", lat: 42.9489, lon: -78.7367, elev_m: 211.0 },
    Site { id: "KBYX", lat: 24.5975, lon: -81.7031, elev_m: 3.0 },
    Site { id: "KCAE", lat: 33.9486, lon: -81.1183, elev_m: 70.0 },
    Site { id: "KCBW", lat: 46.0392, lon: -67.8064, elev_m: 227.0 },
    Site { id: "KCBX", lat: 43.4906, lon: -116.2353, elev_m: 933.0 },
    Site { id: "KCCX", lat: 40.9231, lon: -78.0036, elev_m: 733.0 },
    Site { id: "KCLE", lat: 41.4131, lon: -81.8597, elev_m: 233.0 },
    Site { id: "KCLX", lat: 32.6556, lon: -81.0422, elev_m: 30.0 },
    Site { id: "KCRP", lat: 27.7842, lon: -97.5111, elev_m: 14.0 },
    Site { id: "KCXX", lat: 44.5111, lon: -73.1664, elev_m: 97.0 },
    Site { id: "KCYS", lat: 41.1519, lon: -104.8061, elev_m: 1868.0 },
    Site { id: "KDAX", lat: 38.5011, lon: -121.6778, elev_m: 9.0 },
    Site { id: "KDDC", lat: 37.7608, lon: -99.9689, elev_m: 789.0 },
    Site { id: "KDFX", lat: 29.2728, lon: -100.2806, elev_m: 345.0 },
    Site { id: "KDGX", lat: 32.2797, lon: -89.9844, elev_m: 151.0 },
    Site { id: "KDIX", lat: 39.9469, lon: -74.4108, elev_m: 45.0 },
    Site { id: "KDLH", lat: 46.8369, lon: -92.2097, elev_m: 435.0 },
    Site { id: "KDMX", lat: 41.7311, lon: -93.7228, elev_m: 299.0 },
    Site { id: "KDOX", lat: 38.8256, lon: -75.4400, elev_m: 15.0 },
    Site { id: "KDTX", lat: 42.6997, lon: -83.4717, elev_m: 327.0 },
    Site { id: "KDVN", lat: 41.6117, lon: -90.5808, elev_m: 230.0 },
    Site { id: "KDYX", lat: 32.5383, lon: -99.2542, elev_m: 462.0 },
    Site { id: "KEAX", lat: 38.8103, lon: -94.2644, elev_m: 303.0 },
    Site { id: "KEMX", lat: 31.8936, lon: -110.6303, elev_m: 1586.0 },
    Site { id: "KENX", lat: 42.5864, lon: -74.0639, elev_m: 556.0 },
    Site { id: "KEPZ", lat: 31.8731, lon: -106.6981, elev_m: 1251.0 },
    Site { id: "KESX", lat: 35.7011, lon: -114.8914, elev_m: 1483.0 },
    Site { id: "KEVX", lat: 30.5644, lon: -85.9214, elev_m: 43.0 },
    Site { id: "KEWX", lat: 29.7039, lon: -98.0283, elev_m: 193.0 },
    Site { id: "KEYX", lat: 35.0978, lon: -117.5608, elev_m: 840.0 },
    Site { id: "KFCX", lat: 37.0244, lon: -80.2742, elev_m: 874.0 },
    Site { id: "KFDR", lat: 34.3622, lon: -98.9764, elev_m: 386.0 },
    Site { id: "KFDX", lat: 34.6353, lon: -103.6300, elev_m: 1417.0 },
    Site { id: "KFFC", lat: 33.3636, lon: -84.5658, elev_m: 262.0 },
    Site { id: "KFSD", lat: 43.5878, lon: -96.7294, elev_m: 436.0 },
    Site { id: "KFSX", lat: 34.5744, lon: -111.1981, elev_m: 2261.0 },
    Site { id: "KFTG", lat: 39.7866, lon: -104.5458, elev_m: 1675.0 },
    Site { id: "KFWS", lat: 32.5731, lon: -97.3031, elev_m: 208.0 },
    Site { id: "KGGW", lat: 48.2064, lon: -106.6253, elev_m: 695.0 },
    Site { id: "KGJX", lat: 39.0622, lon: -108.2136, elev_m: 3046.0 },
    Site { id: "KGLD", lat: 39.3667, lon: -101.7003, elev_m: 1113.0 },
    Site { id: "KGRB", lat: 44.4983, lon: -88.1114, elev_m: 208.0 },
    Site { id: "KGRK", lat: 30.7219, lon: -97.3831, elev_m: 164.0 },
    Site { id: "KGRR", lat: 42.8939, lon: -85.5447, elev_m: 237.0 },
    Site { id: "KGSP", lat: 34.8833, lon: -82.2200, elev_m: 286.0 },
    Site { id: "KGWX", lat: 33.8967, lon: -88.3289, elev_m: 145.0 },
    Site { id: "KGYX", lat: 43.8914, lon: -70.2564, elev_m: 125.0 },
    Site { id: "KHDX", lat: 33.0764, lon: -106.1228, elev_m: 1287.0 },
    Site { id: "KHGX", lat: 29.4719, lon: -95.0792, elev_m: 5.0 },
    Site { id: "KHNX", lat: 36.3142, lon: -119.6322, elev_m: 73.0 },
    Site { id: "KHPX", lat: 36.7367, lon: -87.2853, elev_m: 176.0 },
    Site { id: "KHTX", lat: 34.9306, lon: -86.0833, elev_m: 537.0 },
    Site { id: "KICT", lat: 37.6547, lon: -97.4428, elev_m: 407.0 },
    Site { id: "KICX", lat: 37.5908, lon: -112.8622, elev_m: 3231.0 },
    Site { id: "KILN", lat: 39.4203, lon: -83.8217, elev_m: 322.0 },
    Site { id: "KILX", lat: 40.1506, lon: -89.3367, elev_m: 177.0 },
    Site { id: "KIND", lat: 39.7075, lon: -86.2803, elev_m: 241.0 },
    Site { id: "KINX", lat: 36.1750, lon: -95.5642, elev_m: 204.0 },
    Site { id: "KIWA", lat: 33.2892, lon: -111.6700, elev_m: 412.0 },
    Site { id: "KIWX", lat: 41.4089, lon: -85.7000, elev_m: 290.0 },
    Site { id: "KJAX", lat: 30.4847, lon: -81.7019, elev_m: 10.0 },
    Site { id: "KJGX", lat: 32.6753, lon: -83.3511, elev_m: 159.0 },
    Site { id: "KJKL", lat: 37.5908, lon: -83.3131, elev_m: 416.0 },
    Site { id: "KLBB", lat: 33.6542, lon: -101.8142, elev_m: 993.0 },
    Site { id: "KLCH", lat: 30.1253, lon: -93.2158, elev_m: 4.0 },
    Site { id: "KLIX", lat: 30.3367, lon: -89.8256, elev_m: 7.0 },
    Site { id: "KLNX", lat: 41.9578, lon: -100.5764, elev_m: 905.0 },
    Site { id: "KLOT", lat: 41.6044, lon: -88.0847, elev_m: 202.0 },
    Site { id: "KLRX", lat: 40.7397, lon: -116.8028, elev_m: 2056.0 },
    Site { id: "KLSX", lat: 38.6989, lon: -90.6828, elev_m: 185.0 },
    Site { id: "KLTX", lat: 33.9892, lon: -78.4292, elev_m: 20.0 },
    Site { id: "KLVX", lat: 37.9753, lon: -85.9439, elev_m: 219.0 },
    Site { id: "KLWX", lat: 38.9753, lon: -77.4778, elev_m: 83.0 },
    Site { id: "KLZK", lat: 34.8364, lon: -92.2622, elev_m: 173.0 },
    Site { id: "KMAF", lat: 31.9433, lon: -102.1892, elev_m: 874.0 },
    Site { id: "KMAX", lat: 42.0811, lon: -122.7172, elev_m: 2290.0 },
    Site { id: "KMBX", lat: 48.3925, lon: -100.8644, elev_m: 455.0 },
    Site { id: "KMHX", lat: 34.7758, lon: -76.8761, elev_m: 9.0 },
    Site { id: "KMKX", lat: 42.9678, lon: -88.5506, elev_m: 292.0 },
    Site { id: "KMLB", lat: 28.1133, lon: -80.6542, elev_m: 11.0 },
    Site { id: "KMOB", lat: 30.6794, lon: -88.2397, elev_m: 63.0 },
    Site { id: "KMPX", lat: 44.8489, lon: -93.5656, elev_m: 288.0 },
    Site { id: "KMQT", lat: 46.5311, lon: -87.5483, elev_m: 430.0 },
    Site { id: "KMRX", lat: 36.1686, lon: -83.4017, elev_m: 408.0 },
    Site { id: "KMSX", lat: 47.0411, lon: -113.9861, elev_m: 2394.0 },
    Site { id: "KMTX", lat: 41.2628, lon: -112.4478, elev_m: 1969.0 },
    Site { id: "KMUX", lat: 37.1551, lon: -121.8983, elev_m: 1057.0 },
    Site { id: "KMVX", lat: 47.5278, lon: -97.3256, elev_m: 300.0 },
    Site { id: "KMXX", lat: 32.5367, lon: -85.7897, elev_m: 122.0 },
    Site { id: "KNKX", lat: 32.9189, lon: -117.0419, elev_m: 291.0 },
    Site { id: "KNQA", lat: 35.3447, lon: -89.8733, elev_m: 86.0 },
    Site { id: "KOAX", lat: 41.3203, lon: -96.3667, elev_m: 350.0 },
    Site { id: "KOHX", lat: 36.2472, lon: -86.5625, elev_m: 176.0 },
    Site { id: "KOKX", lat: 40.8656, lon: -72.8639, elev_m: 26.0 },
    Site { id: "KOTX", lat: 47.6803, lon: -117.6267, elev_m: 727.0 },
    Site { id: "KPAH", lat: 37.0683, lon: -88.7719, elev_m: 119.0 },
    Site { id: "KPBZ", lat: 40.5317, lon: -80.2181, elev_m: 361.0 },
    Site { id: "KPDT", lat: 45.6906, lon: -118.8528, elev_m: 462.0 },
    Site { id: "KPOE", lat: 31.1556, lon: -92.9758, elev_m: 124.0 },
    Site { id: "KPUX", lat: 38.4595, lon: -104.1814, elev_m: 1600.0 },
    Site { id: "KRAX", lat: 35.6656, lon: -78.4900, elev_m: 106.0 },
    Site { id: "KRGX", lat: 39.7542, lon: -119.4622, elev_m: 2530.0 },
    Site { id: "KRIW", lat: 43.0661, lon: -108.4772, elev_m: 1697.0 },
    Site { id: "KRLX", lat: 38.3111, lon: -81.7228, elev_m: 329.0 },
    Site { id: "KRTX", lat: 45.7150, lon: -122.9653, elev_m: 484.0 },
    Site { id: "KSFX", lat: 43.1058, lon: -112.6861, elev_m: 1364.0 },
    Site { id: "KSGF", lat: 37.2353, lon: -93.4006, elev_m: 390.0 },
    Site { id: "KSHV", lat: 32.4508, lon: -93.8414, elev_m: 83.0 },
    Site { id: "KSJT", lat: 31.3711, lon: -100.4925, elev_m: 576.0 },
    Site { id: "KSOX", lat: 33.8178, lon: -117.6361, elev_m: 923.0 },
    Site { id: "KSRX", lat: 35.2906, lon: -94.3619, elev_m: 195.0 },
    Site { id: "KTBW", lat: 27.7056, lon: -82.4017, elev_m: 12.0 },
    Site { id: "KTFX", lat: 47.4597, lon: -111.3856, elev_m: 1132.0 },
    Site { id: "KTLH", lat: 30.3975, lon: -84.3289, elev_m: 19.0 },
    Site { id: "KTLX", lat: 35.3331, lon: -97.2778, elev_m: 370.0 },
    Site { id: "KTWX", lat: 38.9969, lon: -96.2325, elev_m: 417.0 },
    Site { id: "KTYX", lat: 43.7558, lon: -75.6800, elev_m: 562.0 },
    Site { id: "KUDX", lat: 44.1250, lon: -102.8297, elev_m: 919.0 },
    Site { id: "KUEX", lat: 40.3208, lon: -98.4419, elev_m: 602.0 },
    Site { id: "KVAX", lat: 30.8903, lon: -83.0019, elev_m: 54.0 },
    Site { id: "KVBX", lat: 34.8378, lon: -120.3978, elev_m: 376.0 },
    Site { id: "KVNX", lat: 36.7406, lon: -98.1278, elev_m: 369.0 },
    Site { id: "KVTX", lat: 34.4117, lon: -119.1797, elev_m: 831.0 },
    Site { id: "KVWX", lat: 38.2603, lon: -87.7247, elev_m: 154.0 },
    Site { id: "KYUX", lat: 32.4953, lon: -114.6567, elev_m: 53.0 },
];

pub fn sites() -> &'static [Site] { SITES }

/// Approx great-circle distance (m) on a sphere.
fn haversine_m(lat1: f64, lon1: f64, lat2: f64, lon2: f64) -> f64 {
    const R: f64 = 6_371_000.0;
    let (p1, p2) = (lat1.to_radians(), lat2.to_radians());
    let dp = (lat2 - lat1).to_radians();
    let dl = (lon2 - lon1).to_radians();
    let a = (dp / 2.0).sin().powi(2) + p1.cos() * p2.cos() * (dl / 2.0).sin().powi(2);
    2.0 * R * a.sqrt().asin()
}

/// Sites whose `range_m` coverage disc overlaps the bbox (cheap: test the
/// bbox center against the site, padded by range + bbox half-diagonal).
pub fn sites_overlapping(bbox: &BBox, range_m: f64) -> Vec<Site> {
    let cx = (bbox.west + bbox.east) / 2.0;
    let cy = (bbox.south + bbox.north) / 2.0;
    // bbox half-diagonal in meters (rough): disc∩bbox <=> site within (range + halfdiag) of center.
    let half_diag = haversine_m(bbox.south, bbox.west, bbox.north, bbox.east) / 2.0;
    SITES.iter().cloned()
        .filter(|s| haversine_m(s.lat, s.lon, cy, cx) <= range_m + half_diag)
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn catalog_has_known_sites_and_filters_by_bbox() {
        let all = sites();
        // A few canonical sites must be present.
        assert!(all.iter().any(|s| s.id == "KTLX")); // Oklahoma City
        assert!(all.iter().any(|s| s.id == "KFWS")); // Dallas/Fort Worth
        // Filtering to a small box around KTLX returns KTLX, not far-away sites.
        let bbox = crate::config::BBox { west: -98.5, south: 35.0, east: -97.0, north: 36.0 };
        let near = sites_overlapping(&bbox, 230_000.0);
        assert!(near.iter().any(|s| s.id == "KTLX"));
        assert!(!near.iter().any(|s| s.id == "KMUX")); // SF Bay Area, far away
    }
}
