use crate::field::FieldGrid;
use anyhow::{anyhow, Result};
use gribberish::message::read_messages;
use gribberish::message_metadata::MessageMetadata;

/// Decode a single-message MRMS GRIB2 composite-reflectivity field to dBZ.
/// Sentinels (-999 no coverage, -99 no echo, < -90) -> NaN. GRIB2 encodes
/// longitude in 0..360; we normalize every lon to -180..180 so CONUS lands
/// at ~-127..-65 instead of ~233..295 (review #2).
pub fn decode_grib2(bytes: &[u8]) -> Result<FieldGrid> {
    let msg = read_messages(bytes).next().ok_or_else(|| anyhow!("no GRIB2 message"))?;

    let (rows, cols) = msg.grid_dimensions().map_err(|e| anyhow!("grid dims: {e}"))?; // (ny, nx)
    let meta = MessageMetadata::try_from(&msg).map_err(|e| anyhow!("metadata: {e}"))?;
    let (lats, lons) = meta.latlng(); // flattened, row-major
    let raw = msg.data().map_err(|e| anyhow!("data: {e}"))?; // Vec<f64>, row-major

    let norm_lon = |lon: f64| if lon > 180.0 { lon - 360.0 } else { lon };

    let values: Vec<f64> = raw.into_iter()
        .map(|v| if v <= -90.0 || v.is_nan() { f64::NAN } else { v })
        .collect();

    let lat0 = lats[0];
    let lon0 = norm_lon(lons[0]);
    // Step in lon must be computed on normalized values to avoid a 360 jump.
    let dlon = norm_lon(lons[1]) - lon0;
    let dlat = lats[cols] - lats[0]; // row 0 -> row 1 (assumes >=2 rows)

    Ok(FieldGrid { cols, rows, lon0, lat0, dlon, dlat, values })
}
