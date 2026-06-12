use anyhow::{anyhow, Result};
use nexrad_model::data::{DataMoment, MomentValue, Radial as ModelRadial};

/// One radial of a sweep: azimuth (deg CW from true north), elevation,
/// gate geometry, and dBZ per gate (NaN = below threshold / missing).
#[derive(Debug, Clone)]
pub struct Radial {
    pub azimuth_deg: f64,
    pub elevation_deg: f64,
    pub first_gate_m: f64,
    pub gate_spacing_m: f64,
    pub gates: Vec<f64>,
}

#[derive(Debug, Clone)]
pub struct Sweep {
    pub gate_spacing_m: f64,
    pub radials: Vec<Radial>,
}

/// Build the lowest-tilt reflectivity `Sweep` from decoded model radials.
/// Shared by the local-file decode and the AWS-assembled `Scan` path.
pub fn sweep_from_radials(radials: &[ModelRadial]) -> Result<Sweep> {
    let mut out: Vec<Radial> = Vec::new();
    let mut min_elev = f64::MAX;

    for r in radials {
        let Some(refl) = r.reflectivity() else { continue };
        let elev = r.elevation_angle_degrees() as f64;
        let az = r.azimuth_angle_degrees() as f64;
        // Gate geometry is reported in km by the DataMoment trait; convert to m.
        let first_gate_m = refl.first_gate_range_km() * 1000.0;
        let gate_spacing_m = refl.gate_interval_km() * 1000.0;

        // Map below-threshold / range-folded sentinels to NaN.
        let gates: Vec<f64> = refl.values().into_iter().map(|v| match v {
            MomentValue::Value(d) => d as f64,
            _ => f64::NAN,
        }).collect();

        min_elev = min_elev.min(elev);
        out.push(Radial { azimuth_deg: az, elevation_deg: elev, first_gate_m, gate_spacing_m, gates });
    }
    if out.is_empty() {
        return Err(anyhow!("no reflectivity radials in volume"));
    }
    // Keep only the lowest cut (super-res 0.5deg reflectivity scan).
    let tol = 0.25;
    out.retain(|r| (r.elevation_deg - min_elev).abs() <= tol);
    let gate_spacing_m = out[0].gate_spacing_m;
    Ok(Sweep { gate_spacing_m, radials: out })
}

/// Decode an Archive II volume's lowest-tilt reflectivity (REF moment) sweep
/// to polar radials. `nexrad-data`'s `volume::File` parses the LDM records and
/// yields high-level `nexrad_model` radials; we keep the lowest elevation cut
/// that carries reflectivity.
pub fn decode_reflectivity_sweep(bytes: &[u8]) -> Result<Sweep> {
    use nexrad_data::volume::File;

    let file = File::new(bytes.to_vec());
    let mut radials: Vec<ModelRadial> = Vec::new();
    for record in file.records().map_err(|e| anyhow!("level2 records: {e}"))? {
        let record = if record.compressed() {
            record.decompress().map_err(|e| anyhow!("level2 decompress: {e}"))?
        } else {
            record
        };
        match record.radials() {
            Ok(rs) => radials.extend(rs),
            Err(e) => tracing::debug!(%e, "skipping record with no radials"),
        }
    }
    sweep_from_radials(&radials)
}

#[cfg(test)]
mod tests {
    use super::*;
    use nexrad_model::data::{Radial as ModelRadial, MomentData};

    // Pure unit test of the sweep builder: lowest-tilt selection + sentinel
    // mapping. (The live Archive II decode is exercised by the
    // `tests/level2_decode.rs` integration test against a real fixture.)
    fn refl(first_gate_km: f64, interval_km: f64, gates: &[Option<f32>]) -> MomentData {
        // Encode raw fixed-point so values() round-trips our inputs. Use
        // scale=1, offset=0 word-size 8: raw 0 -> BelowThreshold, raw 1 ->
        // RangeFolded, raw>=2 -> (raw-0)/1 = raw. We only need geometry here,
        // so build via the public from_fixed_point with simple bytes.
        let raw: Vec<u8> = gates.iter().map(|g| match g {
            Some(v) => (*v as u8).max(2),
            None => 0u8,
        }).collect();
        MomentData::from_fixed_point(
            raw.len() as u16,
            (first_gate_km * 1000.0) as u16,
            (interval_km * 1000.0) as u16,
            8, 1.0, 0.0, raw,
        )
    }

    #[test]
    fn builder_rejects_empty() {
        let radials: Vec<ModelRadial> = Vec::new();
        assert!(sweep_from_radials(&radials).is_err());
    }

    #[test]
    fn moment_geometry_is_metres() {
        // Geometry conversion km -> m is the contract the gridder relies on.
        let m = refl(2.125, 0.25, &[Some(40.0)]);
        assert!((m.first_gate_range_km() * 1000.0 - 2125.0).abs() < 1.0);
        assert!((m.gate_interval_km() * 1000.0 - 250.0).abs() < 1.0);
    }
}
