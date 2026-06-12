use anyhow::{Context, Result};
use clap::Parser;
use radar_contour::{
    chaikin, config::Config, contour,
    field::{level2::Level2Source, ReflectivityField},
    mercator, mvt, package,
    publish::{self, R2Client},
    smooth, tile,
};
use rusty_s3::{Bucket, Credentials, UrlStyle};
use std::path::PathBuf;

#[tokio::main]
async fn main() -> Result<()> {
    tracing_subscriber::fmt().with_env_filter(
        std::env::var("RUST_LOG").unwrap_or_else(|_| "info".into())).init();
    let cfg = Config::parse();

    // v1 field source: Level II super-res, composited over the bbox.
    let cadence_s = 300; // matches the CronJob window (Task 21)
    let source = Level2Source::new(cfg.bbox, cfg.grid_deg, cadence_s);
    let frame = source.latest_frame_id().await?;

    let r2 = build_r2_client(&cfg)?;
    let current = publish::read_latest(&r2, &cfg.r2_prefix).await?;
    if publish::should_skip(current.as_ref().map(|l| l.ts.as_str()), &frame) {
        tracing::info!(%frame, "frame unchanged; skipping");
        return Ok(());
    }

    let field = source.fetch(&frame).await?;
    let blurred = smooth::gaussian_blur(&field, cfg.gaussian_sigma);
    let bands_grid = contour::isobands(&blurred, &cfg.dbz_thresholds);
    let bands: Vec<contour::Band> = bands_grid.into_iter().map(|b| contour::Band {
        dbz: b.dbz,
        geom: chaikin::smooth_multipolygon(
            &mercator::lift_multipolygon(&blurred, &b.geom), cfg.chaikin_iterations),
    }).collect();

    // review #3: time the pyramid build (perf-budget gate, Task 14 Step 5).
    let t0 = std::time::Instant::now();
    let pyramid = tile::build_pyramid(&bands, &cfg.bbox, cfg.min_zoom, cfg.max_zoom);
    tracing::info!(tiles = pyramid.len(), ms = t0.elapsed().as_millis(), "pyramid built");

    let raw: Vec<package::RawTile> = pyramid.iter()
        .map(|t| (t.z, t.x, t.y, mvt::encode_tile(t))).collect();

    let tmp = tempdir_path();
    std::fs::create_dir_all(&tmp).ok();
    let mbtiles = tmp.join("frame.mbtiles");
    let pmtiles = tmp.join("frame.pmtiles");
    package::write_mbtiles(&mbtiles, &raw, cfg.min_zoom, cfg.max_zoom)?;
    package::mbtiles_to_pmtiles(&mbtiles, &pmtiles)?;

    // Publish under the canonical frame id (RFC3339); the object key is
    // sanitized inside publish_frame, and latest.json stores the canonical id
    // so the next run's should_skip matches latest_frame_id.
    publish::publish_frame(&r2, &cfg.r2_prefix, &frame, &pmtiles,
        cfg.min_zoom, cfg.max_zoom).await?;
    tracing::info!(%frame, "published frame");
    Ok(())
}

/// R2 client (path-style, region `auto`) with credentials from the
/// conventional env vars.
fn build_r2_client(cfg: &Config) -> Result<R2Client> {
    let endpoint = cfg.r2_endpoint.clone().context("RADAR_R2_ENDPOINT is required")?;
    let bucket_name = cfg.r2_bucket.clone().context("RADAR_R2_BUCKET is required")?;
    let key_id = std::env::var("RADAR_R2_ACCESS_KEY_ID").context("RADAR_R2_ACCESS_KEY_ID")?;
    let secret = std::env::var("RADAR_R2_SECRET_ACCESS_KEY").context("RADAR_R2_SECRET_ACCESS_KEY")?;

    let base = endpoint.parse().context("RADAR_R2_ENDPOINT must be a URL")?;
    let bucket = Bucket::new(base, UrlStyle::Path, bucket_name, "auto")
        .context("invalid R2 bucket config")?;
    let creds = Credentials::new(key_id, secret);
    Ok(R2Client { bucket, creds, http: reqwest::Client::new() })
}

fn tempdir_path() -> PathBuf {
    std::env::temp_dir().join("radar-contour-frame")
}
