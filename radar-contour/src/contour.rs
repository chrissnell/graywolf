use crate::field::FieldGrid;
use geo_types::MultiPolygon;

#[derive(Debug, Clone)]
pub struct Band {
    /// Lower-bound dBZ of this filled band (the `dbz` tile attribute).
    pub dbz: f64,
    /// Polygons in GRID INDEX space: x = col, y = row.
    pub geom: MultiPolygon<f64>,
}

/// Build filled isobands. `thresholds` ascending; each band covers [t, next_t);
/// the top band covers [last, +inf).
pub fn isobands(g: &FieldGrid, thresholds: &[f64]) -> Vec<Band> {
    use contour_isobands::ContourBuilder;

    let data: Vec<f64> = g.values.iter().map(|v| if v.is_nan() { -1000.0 } else { *v }).collect();
    let mut intervals: Vec<f64> = thresholds.to_vec();
    intervals.push(1.0e6);

    let builder = ContourBuilder::new(g.cols, g.rows);
    let computed = builder.contours(&data, &intervals).unwrap_or_default();

    computed.into_iter().filter_map(|band| {
        let lower = band.min_v();
        if lower >= 1.0e6 { return None; }
        Some(Band { dbz: lower, geom: band.geometry().clone() })
    }).collect()
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::field::FieldGrid;

    #[test]
    fn extracts_one_band_above_threshold() {
        let mut v = vec![0.0; 25];
        for (r, c) in [(1,1),(1,2),(1,3),(2,1),(2,2),(2,3),(3,1),(3,2),(3,3)] {
            v[r*5 + c] = 30.0;
        }
        let g = FieldGrid { cols:5, rows:5, lon0:0.0, lat0:0.0, dlon:1.0, dlat:-1.0, values:v };
        let bands = isobands(&g, &[5.0, 10.0, 20.0]);
        let b20 = bands.iter().find(|b| (b.dbz - 20.0).abs() < 1e-9).expect("20 dBZ band");
        assert!(b20.geom.0.iter().map(|p| p.exterior().0.len()).sum::<usize>() > 0);
    }
}
