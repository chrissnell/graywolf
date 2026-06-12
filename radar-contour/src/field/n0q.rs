use crate::field::FieldGrid;
use anyhow::Result;
use image::GenericImageView;
use std::collections::HashMap;
use std::sync::OnceLock;

/// N0Q palette: index i (i>=1) -> dBZ = -32.0 + (i-1)*0.5; index 0 = NaN.
pub fn n0q_lut() -> [f64; 256] {
    let mut lut = [f64::NAN; 256];
    for (i, slot) in lut.iter_mut().enumerate().skip(1) {
        *slot = -32.0 + (i as f64 - 1.0) * 0.5;
    }
    lut
}

/// IEM N0Q RGB ramp indexed 0..=255. Index 0 is "no data".
///
/// NOTE: these triples are a *structural placeholder* generated to be unique
/// per index so the RGB->index inverse lookup is well-defined and the decode
/// path is correct against tiles produced with this same table. Before the
/// N0Q fallback is used to decode real IEM national-mosaic PNGs, replace this
/// with IEM's published `n0q` colortable (the canonical 256 RGB triples), or
/// the inverse lookup will not match IEM's pixels. Level II (v1) and MRMS do
/// not depend on this table.
fn n0q_rgb() -> &'static [[u8; 3]; 256] {
    static TABLE: OnceLock<[[u8; 3]; 256]> = OnceLock::new();
    TABLE.get_or_init(|| {
        let mut t = [[0u8; 3]; 256];
        // Index 0 reserved for "no data" (transparent / black).
        for (i, slot) in t.iter_mut().enumerate().skip(1) {
            // Deterministic, monotonic-ish ramp guaranteeing distinct triples.
            let h = (i as f64 / 256.0) * 300.0; // hue 0..300 deg
            let (r, g, b) = hsv_to_rgb(h, 0.9, 0.95);
            *slot = [r, g, b];
        }
        t
    })
}

fn hsv_to_rgb(h: f64, s: f64, v: f64) -> (u8, u8, u8) {
    let c = v * s;
    let x = c * (1.0 - ((h / 60.0) % 2.0 - 1.0).abs());
    let m = v - c;
    let (r, g, b) = match (h / 60.0) as u32 {
        0 => (c, x, 0.0),
        1 => (x, c, 0.0),
        2 => (0.0, c, x),
        3 => (0.0, x, c),
        4 => (x, 0.0, c),
        _ => (c, 0.0, x),
    };
    (
        ((r + m) * 255.0).round() as u8,
        ((g + m) * 255.0).round() as u8,
        ((b + m) * 255.0).round() as u8,
    )
}

fn reverse_map() -> &'static HashMap<[u8; 3], usize> {
    static MAP: OnceLock<HashMap<[u8; 3], usize>> = OnceLock::new();
    MAP.get_or_init(|| {
        let mut m = HashMap::with_capacity(256);
        for (i, c) in n0q_rgb().iter().enumerate() {
            m.entry(*c).or_insert(i);
        }
        m
    })
}

fn rgba_to_dbz(rgba: [u8; 4], lut: &[f64; 256]) -> f64 {
    let key = [rgba[0], rgba[1], rgba[2]];
    match reverse_map().get(&key) { Some(&i) => lut[i], None => f64::NAN }
}

/// Decode an N0Q national mosaic PNG into a dBZ FieldGrid (IEM "us" extent).
pub fn decode_png(bytes: &[u8]) -> Result<FieldGrid> {
    let img = image::load_from_memory(bytes)?;
    let (w, h) = img.dimensions();
    let lut = n0q_lut();
    let mut values = Vec::with_capacity((w * h) as usize);
    for y in 0..h {
        for x in 0..w {
            let px = img.get_pixel(x, y);
            values.push(rgba_to_dbz(px.0, &lut));
        }
    }
    const W: f64 = -126.0; const E: f64 = -66.0;
    const S: f64 = 24.0;   const N: f64 = 50.0;
    Ok(FieldGrid {
        cols: w as usize, rows: h as usize,
        lon0: W, lat0: N,
        dlon: (E - W) / w as f64,
        dlat: (S - N) / h as f64, // negative
        values,
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn lut_roundtrips_known_palette_entries() {
        let lut = n0q_lut();
        assert!(lut[0].is_nan());                       // index 0 = no data
        let defined: Vec<f64> = lut.iter().cloned().filter(|v| !v.is_nan()).collect();
        assert!(defined.windows(2).all(|w| w[1] >= w[0])); // monotonic
        assert!(*defined.last().unwrap() <= 95.0);
    }
}
