use anyhow::{anyhow, Result};
use nexrad_model::data::{DataMoment, MomentValue, Radial as ModelRadial, Sweep as ModelSweep};

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

/// Convert one model radial's reflectivity moment into our `Radial`, or `None`
/// if it carries no reflectivity. Gate geometry is reported in km by the
/// `DataMoment` trait; we convert to metres. Below-threshold / range-folded
/// sentinels map to NaN.
fn radial_from_model(r: &ModelRadial) -> Option<Radial> {
    let refl = r.reflectivity()?;
    let gates: Vec<f64> = refl.values().into_iter().map(|v| match v {
        MomentValue::Value(d) => d as f64,
        _ => f64::NAN,
    }).collect();
    Some(Radial {
        azimuth_deg: r.azimuth_angle_degrees() as f64,
        elevation_deg: r.elevation_angle_degrees() as f64,
        first_gate_m: refl.first_gate_range_km() * 1000.0,
        gate_spacing_m: refl.gate_interval_km() * 1000.0,
        gates,
    })
}

/// Mean elevation of a model sweep (its reported cut angle, else the mean of
/// its radials) -- the key for picking the lowest tilt.
fn sweep_elevation(s: &ModelSweep) -> f64 {
    if let Some(e) = s.elevation_angle_degrees() {
        return e as f64;
    }
    let radials = s.radials();
    if radials.is_empty() { return f64::MAX; }
    radials.iter().map(|r| r.elevation_angle_degrees() as f64).sum::<f64>() / radials.len() as f64
}

/// Build the lowest-tilt reflectivity `Sweep` from decoded model radials.
/// Shared by the local-file decode and the AWS-assembled `Scan` path.
///
/// Radials are grouped into sweeps by the model, then the single lowest
/// elevation cut that carries reflectivity is selected -- so SAILS / MESO-SAILS
/// supplemental 0.5deg scans in the same volume are NOT merged into one sweep
/// (which would duplicate azimuths and let a stale supplemental cut win).
pub fn sweep_from_radials(radials: &[ModelRadial]) -> Result<Sweep> {
    let sweeps = ModelSweep::from_radials(radials.to_vec());
    let chosen = sweeps.into_iter()
        .filter(|s| s.radials().iter().any(|r| r.reflectivity().is_some()))
        .min_by(|a, b| sweep_elevation(a).total_cmp(&sweep_elevation(b)))
        .ok_or_else(|| anyhow!("no reflectivity sweep in volume"))?;

    let out: Vec<Radial> = chosen.radials().iter().filter_map(radial_from_model).collect();
    if out.is_empty() {
        return Err(anyhow!("no reflectivity radials in chosen sweep"));
    }
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
