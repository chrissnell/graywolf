use radar_contour::field::level2::decode::decode_reflectivity_sweep;

// Real Archive II decode against a trimmed fixture. The fixture is a real
// Level II volume chunk for one site (see plan Task 5 Step 1):
//
//   aws s3 cp --no-sign-request \
//     s3://unidata-nexrad-level2/<YYYY>/<MM>/<DD>/KTLX/<...V06> \
//     radar-contour/tests/fixtures/level2_tiny
//
// When the fixture is absent (e.g. offline CI), the test skips rather than
// failing, so the suite stays green without network access.
#[test]
fn decodes_lowest_tilt_reflectivity() {
    let path = "tests/fixtures/level2_tiny";
    let Ok(bytes) = std::fs::read(path) else {
        eprintln!("skipping: fixture {path} absent (download a Level II volume to enable)");
        return;
    };
    let sweep = decode_reflectivity_sweep(&bytes).expect("decode");
    // Super-res: ~720 radials (0.5deg) and 250 m gates.
    assert!(sweep.radials.len() > 300);
    assert!(sweep.gate_spacing_m > 0.0 && sweep.gate_spacing_m <= 300.0);
    // Lowest tilt ~0.5deg.
    assert!(sweep.radials[0].elevation_deg < 2.0);
    // Some real echo present and in a sane dBZ range.
    let any = sweep.radials.iter().flat_map(|r| r.gates.iter())
        .cloned().filter(|v| !v.is_nan());
    let max = any.fold(f64::MIN, f64::max);
    assert!((0.0..=95.0).contains(&max));
}
