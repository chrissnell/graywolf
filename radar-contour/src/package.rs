use anyhow::{anyhow, Result};
use rusqlite::Connection;
use std::path::Path;
use std::process::Command;

pub type RawTile = (u8, u32, u32, Vec<u8>);

/// Standard MBTiles, TMS y (tms_y = 2^z - 1 - xyz_y), gzip'd pbf tiles.
pub fn write_mbtiles(path: &Path, tiles: &[RawTile], minz: u8, maxz: u8) -> Result<()> {
    let conn = Connection::open(path)?;
    conn.execute_batch(
        "PRAGMA journal_mode=OFF;
         CREATE TABLE metadata (name TEXT, value TEXT);
         CREATE TABLE tiles (zoom_level INTEGER, tile_column INTEGER,
            tile_row INTEGER, tile_data BLOB);
         CREATE UNIQUE INDEX tile_index ON tiles
            (zoom_level, tile_column, tile_row);",
    )?;
    {
        let mut md = conn.prepare("INSERT INTO metadata (name,value) VALUES (?,?)")?;
        for (k, v) in [
            ("name", "radar"),
            ("format", "pbf"),
            ("minzoom", &minz.to_string()[..]),
            ("maxzoom", &maxz.to_string()[..]),
            ("type", "overlay"),
            ("json", r#"{"vector_layers":[{"id":"radar","fields":{"dbz":"Number"}}]}"#),
        ] {
            md.execute(rusqlite::params![k, v])?;
        }
    }
    {
        let mut ins = conn.prepare("INSERT OR REPLACE INTO tiles VALUES (?,?,?,?)")?;
        for (z, x, y, data) in tiles {
            let tms_y = (1u32 << *z) - 1 - *y;
            let gz = gzip(data)?;
            ins.execute(rusqlite::params![z, x, tms_y, gz])?;
        }
    }
    Ok(())
}

fn gzip(data: &[u8]) -> Result<Vec<u8>> {
    use flate2::{write::GzEncoder, Compression};
    use std::io::Write;
    let mut e = GzEncoder::new(Vec::new(), Compression::default());
    e.write_all(data)?;
    Ok(e.finish()?)
}

pub fn mbtiles_to_pmtiles(mbtiles: &Path, pmtiles: &Path) -> Result<()> {
    let status = Command::new("pmtiles").arg("convert").arg(mbtiles).arg(pmtiles).status()?;
    if !status.success() { return Err(anyhow!("pmtiles convert failed: {status}")); }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn writes_mbtiles_with_tiles_and_metadata() {
        let raw = vec![(3u8, 1u32, 2u32, vec![1, 2, 3u8])];
        let dir = std::env::temp_dir().join("radar_mbtiles_test");
        let _ = std::fs::remove_dir_all(&dir);
        std::fs::create_dir_all(&dir).unwrap();
        let path = dir.join("frame.mbtiles");
        write_mbtiles(&path, &raw, 3, 10).unwrap();
        let conn = rusqlite::Connection::open(&path).unwrap();
        let n: i64 = conn.query_row("SELECT count(*) FROM tiles", [], |r| r.get(0)).unwrap();
        assert_eq!(n, 1);
        let fmt: String = conn.query_row(
            "SELECT value FROM metadata WHERE name='format'", [], |r| r.get(0)).unwrap();
        assert_eq!(fmt, "pbf");
    }
}
