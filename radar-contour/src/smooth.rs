use crate::field::FieldGrid;

fn kernel(sigma: f64) -> Vec<f64> {
    let radius = (3.0 * sigma).ceil() as i64;
    let mut k: Vec<f64> = (-radius..=radius)
        .map(|i| (-(i as f64 * i as f64) / (2.0 * sigma * sigma)).exp())
        .collect();
    let sum: f64 = k.iter().sum();
    for x in &mut k { *x /= sum; }
    k
}

/// Separable Gaussian blur. NaN -> 0 before blurring (no-echo background).
pub fn gaussian_blur(g: &FieldGrid, sigma: f64) -> FieldGrid {
    let k = kernel(sigma);
    let r = (k.len() / 2) as i64;
    let (cols, rows) = (g.cols as i64, g.rows as i64);
    let src: Vec<f64> = g.values.iter().map(|v| if v.is_nan() { 0.0 } else { *v }).collect();

    let mut tmp = vec![0.0f64; src.len()];
    for row in 0..rows {
        for col in 0..cols {
            let mut acc = 0.0;
            for (j, w) in k.iter().enumerate() {
                let cc = (col + j as i64 - r).clamp(0, cols - 1);
                acc += src[(row * cols + cc) as usize] * w;
            }
            tmp[(row * cols + col) as usize] = acc;
        }
    }
    let mut out = vec![0.0f64; src.len()];
    for row in 0..rows {
        for col in 0..cols {
            let mut acc = 0.0;
            for (j, w) in k.iter().enumerate() {
                let rr = (row + j as i64 - r).clamp(0, rows - 1);
                acc += tmp[(rr * cols + col) as usize] * w;
            }
            out[(row * cols + col) as usize] = acc;
        }
    }
    FieldGrid { values: out, ..g.clone() }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::field::FieldGrid;

    fn grid(cols: usize, rows: usize, v: Vec<f64>) -> FieldGrid {
        FieldGrid { cols, rows, lon0: 0.0, lat0: 0.0, dlon: 1.0, dlat: -1.0, values: v }
    }

    #[test]
    fn blur_spreads_a_single_spike() {
        let mut v = vec![0.0; 25];
        v[12] = 50.0;
        let g = grid(5, 5, v);
        let out = gaussian_blur(&g, 1.0);
        assert!(out.at(2, 2) < 50.0 && out.at(2, 2) > 0.0);
        assert!(out.at(1, 2) > 0.0);
        let sum_in: f64 = g.values.iter().sum();
        let sum_out: f64 = out.values.iter().sum();
        assert!((sum_in - sum_out).abs() < 1.0);
    }

    #[test]
    fn nan_is_treated_as_zero_then_does_not_panic() {
        let mut v = vec![f64::NAN; 9];
        v[4] = 30.0;
        let g = grid(3, 3, v);
        let out = gaussian_blur(&g, 1.0);
        assert!(out.values.iter().all(|x| x.is_finite()));
    }
}
