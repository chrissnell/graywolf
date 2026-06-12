use geo_types::{Coord, LineString, MultiPolygon, Polygon};

fn chaikin_once(ring: &LineString<f64>) -> LineString<f64> {
    let pts = &ring.0;
    if pts.len() < 4 { return ring.clone(); }
    let mut out: Vec<Coord<f64>> = Vec::with_capacity(pts.len() * 2);
    for w in pts.windows(2) {
        let (p, q) = (w[0], w[1]);
        out.push(Coord { x: 0.75 * p.x + 0.25 * q.x, y: 0.75 * p.y + 0.25 * q.y });
        out.push(Coord { x: 0.25 * p.x + 0.75 * q.x, y: 0.25 * p.y + 0.75 * q.y });
    }
    out.push(out[0]);
    LineString(out)
}

pub fn chaikin_ring(ring: &LineString<f64>, iterations: u8) -> LineString<f64> {
    let mut r = ring.clone();
    for _ in 0..iterations { r = chaikin_once(&r); }
    r
}

pub fn smooth_multipolygon(mp: &MultiPolygon<f64>, iterations: u8) -> MultiPolygon<f64> {
    MultiPolygon(mp.0.iter().map(|poly| {
        Polygon::new(
            chaikin_ring(poly.exterior(), iterations),
            poly.interiors().iter().map(|h| chaikin_ring(h, iterations)).collect(),
        )
    }).collect())
}

#[cfg(test)]
mod tests {
    use super::*;
    use geo_types::{Coord, LineString};

    #[test]
    fn chaikin_increases_vertex_count_and_stays_closed() {
        let sq = LineString(vec![
            Coord{x:0.0,y:0.0}, Coord{x:1.0,y:0.0},
            Coord{x:1.0,y:1.0}, Coord{x:0.0,y:1.0}, Coord{x:0.0,y:0.0},
        ]);
        let out = chaikin_ring(&sq, 2);
        assert!(out.0.len() > sq.0.len());
        assert_eq!(out.0.first(), out.0.last());
    }
}
