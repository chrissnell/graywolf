pub mod sites;
pub mod decode;
pub mod grid;

use crate::config::BBox;
use crate::field::{FieldGrid, GridSpec, ReflectivityField};
use anyhow::{anyhow, Result};
use decode::Sweep;
use sites::Site;

const COVERAGE_M: f64 = 230_000.0;

pub struct Level2Source {
    bbox: BBox,
    grid_deg: f64,
    cadence_s: i64,
}

impl Level2Source {
    pub fn new(bbox: BBox, grid_deg: f64, cadence_s: i64) -> Self {
        Self { bbox, grid_deg, cadence_s }
    }
}

/// Round an RFC3339 timestamp down to the nearest `window_s` boundary. Pure
/// parse math (see `crate::timeutil`) so it is unit-testable without a clock.
pub fn cadence_bucket(rfc3339: &str, window_s: i64) -> String {
    crate::timeutil::floor_rfc3339(rfc3339, window_s).unwrap_or_else(|_| rfc3339.to_string())
}

/// Composite already-decoded (site, sweep) pairs into one FieldGrid.
pub fn composite_sweeps(spec: &GridSpec, sweeps: &[(Site, Sweep)]) -> FieldGrid {
    let mut field = spec.empty_field();
    for (site, sweep) in sweeps {
        grid::composite_site(&mut field, spec, site, sweep);
    }
    field
}

/// Newest scan wall-clock time (RFC3339) across the given sites. Lists each
/// site's latest realtime volume and takes the max chunk upload time.
async fn newest_volume_time(sites: &[Site]) -> Result<String> {
    use nexrad_data::aws::realtime::{get_latest_volume, list_chunks_in_volume};

    let mut stamps: Vec<String> = Vec::new();
    for site in sites {
        let latest = match get_latest_volume(site.id).await {
            Ok(l) => l,
            Err(e) => { tracing::warn!(site = site.id, %e, "latest volume lookup failed"); continue; }
        };
        let Some(vol) = latest.volume else { continue };
        match list_chunks_in_volume(site.id, vol, 1000).await {
            Ok(ids) => {
                for c in &ids {
                    if let Some(t) = c.upload_date_time() {
                        stamps.push(t.to_rfc3339());
                    }
                }
            }
            Err(e) => tracing::warn!(site = site.id, %e, "chunk listing failed"),
        }
    }
    // RFC3339 in a single (UTC) offset sorts lexically by time.
    stamps.into_iter().max().ok_or_else(|| anyhow!("no volumes found across sites"))
}

/// Download + assemble the latest realtime volume for a site, then extract the
/// lowest-tilt reflectivity sweep.
async fn fetch_and_decode_site(site: &Site) -> Result<Sweep> {
    use nexrad_data::aws::realtime::{
        assemble_volume, download_chunk, get_latest_volume, list_chunks_in_volume,
    };

    let latest = get_latest_volume(site.id).await
        .map_err(|e| anyhow!("latest volume {}: {e}", site.id))?;
    let vol = latest.volume.ok_or_else(|| anyhow!("no latest volume for {}", site.id))?;
    let chunk_ids = list_chunks_in_volume(site.id, vol, 1000).await
        .map_err(|e| anyhow!("list chunks {}: {e}", site.id))?;

    let mut chunks = Vec::with_capacity(chunk_ids.len());
    for cid in &chunk_ids {
        let (_id, chunk) = download_chunk(site.id, cid).await
            .map_err(|e| anyhow!("download chunk {}: {e}", site.id))?;
        chunks.push(chunk);
    }
    let scan = assemble_volume(chunks).map_err(|e| anyhow!("assemble {}: {e}", site.id))?;
    let radials: Vec<_> = scan.sweeps().iter()
        .flat_map(|s| s.radials().iter().cloned())
        .collect();
    decode::sweep_from_radials(&radials)
}

#[async_trait::async_trait]
impl ReflectivityField for Level2Source {
    async fn latest_frame_id(&self) -> Result<String> {
        let chosen = sites::sites_overlapping(&self.bbox, COVERAGE_M);
        let newest = newest_volume_time(&chosen).await?; // RFC3339
        Ok(cadence_bucket(&newest, self.cadence_s))
    }

    async fn fetch(&self, _frame_id: &str) -> Result<FieldGrid> {
        let spec = GridSpec::from_bbox(&self.bbox, self.grid_deg);
        let chosen = sites::sites_overlapping(&self.bbox, COVERAGE_M);

        // Concurrent fetch+decode per site (this is why the trait is async --
        // review #1). Bound concurrency so we don't open ~160 sockets at once.
        use futures::stream::{self, StreamExt};
        let decoded: Vec<(Site, Sweep)> = stream::iter(chosen)
            .map(|site| async move {
                match fetch_and_decode_site(&site).await {
                    Ok(sweep) => Some((site, sweep)),
                    Err(e) => { tracing::warn!(site = site.id, %e, "site skipped"); None }
                }
            })
            .buffer_unordered(16)
            .collect::<Vec<_>>().await
            .into_iter().flatten().collect();

        tracing::info!(sites = decoded.len(), "sites composited");
        Ok(composite_sweeps(&spec, &decoded))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn frame_id_rounds_to_cadence_window() {
        // 2026-06-12T01:07:43Z with a 300 s window -> 01:05:00Z bucket.
        let id = cadence_bucket("2026-06-12T01:07:43Z", 300);
        assert_eq!(id, "2026-06-12T01:05:00Z");
    }

    #[test]
    fn composite_of_no_sites_is_all_nan() {
        let spec = crate::field::GridSpec::from_bbox(
            &crate::config::BBox { west: -98.0, south: 34.0, east: -96.0, north: 36.0 }, 0.05);
        let field = composite_sweeps(&spec, &[]);
        assert!(field.values.iter().all(|v| v.is_nan()));
    }
}
