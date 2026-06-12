use crate::field::{FieldGrid, GridSpec};
use crate::field::level2::decode::Sweep;
use crate::field::level2::sites::Site;

const EARTH_R: f64 = 6_371_000.0;
const KE: f64 = 4.0 / 3.0; // effective-earth-radius beam model

/// Ground (great-circle) range for a slant range at a beam elevation, using
/// the standard 4/3-earth approximation. At <=230 km this is within a few
/// hundred meters of the rigorous value -- well under one 250 m gate.
fn ground_range_m(slant_m: f64, elev_deg: f64) -> f64 {
    let ae = KE * EARTH_R;
    let el = elev_deg.to_radians();
    // s = ae * atan( r*cos(el) / (ae + r*sin(el)) )
    ae * ((slant_m * el.cos()) / (ae + slant_m * el.sin())).atan()
}

/// Destination lon/lat from an origin, a bearing (deg CW from true north),
/// and a ground distance -- spherical direct (haversine) formula.
fn dest_lonlat(lat0: f64, lon0: f64, bearing_deg: f64, dist_m: f64) -> (f64, f64) {
    let ang = dist_m / EARTH_R;
    let br = bearing_deg.to_radians();
    let (p0, l0) = (lat0.to_radians(), lon0.to_radians());
    let lat = (p0.sin() * ang.cos() + p0.cos() * ang.sin() * br.cos()).asin();
    let lon = l0 + (br.sin() * ang.sin() * p0.cos())
        .atan2(ang.cos() - p0.sin() * lat.sin());
    (lon.to_degrees(), lat.to_degrees())
}

/// Geolocate every gate of `sweep` from `site` and max-combine its dBZ into
/// `field` at the nearest `spec` cell. NaN gates are skipped.
pub fn composite_site(field: &mut FieldGrid, spec: &GridSpec, site: &Site, sweep: &Sweep) {
    for radial in &sweep.radials {
        for (i, &dbz) in radial.gates.iter().enumerate() {
            if dbz.is_nan() { continue; }
            let slant = radial.first_gate_m + i as f64 * radial.gate_spacing_m;
            let ground = ground_range_m(slant, radial.elevation_deg);
            let (lon, lat) = dest_lonlat(site.lat, site.lon, radial.azimuth_deg, ground);
            if let Some((col, row)) = spec.cell_index(lon, lat) {
                let idx = row * spec.cols + col;
                let cur = field.values[idx];
                field.values[idx] = if cur.is_nan() { dbz } else { cur.max(dbz) };
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::field::GridSpec;
    use crate::field::level2::decode::{Radial, Sweep};
    use crate::field::level2::sites::Site;

    fn site() -> Site { Site { id: "KTST", lat: 35.0, lon: -97.0, elev_m: 0.0 } }

    #[test]
    fn gate_due_east_lands_east_of_site() {
        // One radial, azimuth 90deg (due east), a single 50 dBZ gate at ~50 km.
        let sweep = Sweep {
            gate_spacing_m: 250.0,
            radials: vec![Radial {
                azimuth_deg: 90.0, elevation_deg: 0.5,
                first_gate_m: 50_000.0, gate_spacing_m: 250.0,
                gates: vec![50.0],
            }],
        };
        let spec = GridSpec::from_bbox(
            &crate::config::BBox { west: -98.0, south: 34.0, east: -96.0, north: 36.0 }, 0.01);
        let mut field = spec.empty_field();
        composite_site(&mut field, &spec, &site(), &sweep);
        // Find the populated cell; it must be EAST (lon > -97) and near lat 35.
        let mut hit = None;
        for row in 0..spec.rows {
            for col in 0..spec.cols {
                if field.at(col, row) == 50.0 { hit = Some(field.cell_center(col, row)); }
            }
        }
        let (lon, lat) = hit.expect("a populated cell");
        assert!(lon > -97.0, "gate must be east of site, got lon {lon}");
        assert!((lat - 35.0).abs() < 0.2, "due-east gate stays near site lat");
    }

    #[test]
    fn max_combine_keeps_stronger_echo() {
        let spec = GridSpec::from_bbox(
            &crate::config::BBox { west: -98.0, south: 34.0, east: -96.0, north: 36.0 }, 0.01);
        let mut field = spec.empty_field();
        let mk = |dbz: f64| Sweep { gate_spacing_m: 250.0, radials: vec![Radial {
            azimuth_deg: 90.0, elevation_deg: 0.5, first_gate_m: 50_000.0,
            gate_spacing_m: 250.0, gates: vec![dbz] }] };
        composite_site(&mut field, &spec, &site(), &mk(30.0));
        composite_site(&mut field, &spec, &site(), &mk(45.0));
        composite_site(&mut field, &spec, &site(), &mk(40.0));
        let max = field.values.iter().cloned().filter(|v| !v.is_nan()).fold(f64::MIN, f64::max);
        assert_eq!(max, 45.0); // strongest echo wins
    }
}
