use radar_contour::field::mrms::decode_grib2;

// MRMS GRIB2 fallback decode against a real fixture (see plan Task 8 Step 1):
//
//   aws s3 cp --no-sign-request \
//     s3://noaa-mrms-pds/CONUS/MergedReflectivityQComposite_00.50/<latest>.grib2.gz .
//   gunzip <...>.grib2.gz -> radar-contour/tests/fixtures/mrms_tiny.grib2
//
// Skips when the fixture is absent so the suite stays green offline.
#[test]
fn decodes_mrms_composite_to_conus_dbz_grid() {
    let path = "tests/fixtures/mrms_tiny.grib2";
    let Ok(bytes) = std::fs::read(path) else {
        eprintln!("skipping: fixture {path} absent (download an MRMS GRIB2 to enable)");
        return;
    };
    let grid = decode_grib2(&bytes).expect("decode");
    assert!(grid.cols > 100 && grid.rows > 100);
    // review #2: longitudes normalized into -180..180; CONUS origin sane.
    assert!(grid.lon0 > -127.0 && grid.lon0 < -65.0, "lon0 in CONUS, got {}", grid.lon0);
    assert!(grid.values.iter().any(|v| v.is_nan())); // sentinels -> NaN
    let max = grid.values.iter().cloned().filter(|v| !v.is_nan()).fold(f64::MIN, f64::max);
    assert!((0.0..=95.0).contains(&max));
    assert!(grid.dlat < 0.0); // marches south
}
