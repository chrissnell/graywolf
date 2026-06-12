# Smoothed NEXRAD Radar: Vector Contour Tile Pipeline (v1 = Level II super-res) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stand up a server-side pipeline that turns live **Level II super-resolution** reflectivity (per-site Archive II, 250 m gates) into a composited dBZ field, extracts smoothed vector isobands, packages each frame as a single PMTiles archive in R2, range-serves it through the existing origin Worker, and renders it client-side as recolorable MapLibre fill layers.

**Architecture:** A new Rust crate (`radar-contour/`, a member of the existing repo Cargo workspace) runs as a k8s CronJob on `big-bulky-1`. Each tick it: discovers the newest Level II volume for each NEXRAD site overlapping the configured bbox (AWS `unidata-nexrad-level2`, anonymous), decodes the lowest-tilt super-res reflectivity sweep per site, geolocates each polar gate and **max-composites all sites onto one regular lon/lat dBZ grid**, Gaussian-blurs the grid, extracts filled isobands at NWS dBZ breakpoints, Chaikin-smooths the band boundaries, reprojects + clips to a Web Mercator tile pyramid (z3–z10), encodes MVT, packs the whole pyramid into one `.pmtiles` archive, and publishes it atomically to R2 (`radar/<ts>.pmtiles` written first, `radar/latest.json` pointer flipped last). The existing graywolf-maps origin Worker gains a radar route that range-reads individual tiles out of the archive. The graywolf web client gets a vector source + `fill` layers whose `fill-color` is a `step` expression on each polygon's `dbz` attribute, polling `latest.json` to advance frames.

**Tech Stack:** Rust (`nexrad-data` for AWS Level II access, `nexrad-decode`/`nexrad-model` for Archive II message-31 decode, `image` for the N0Q PNG fallback, `gribberish` for the MRMS GRIB2 fallback, `contour-isobands` for filled bands, `geo` for geometry + clipping, `geozero`/mvt for tile encoding, `rusqlite` for MBTiles staging, the `pmtiles` CLI for MBTiles→PMTiles conversion, `aws-sdk-s3` for R2). Cloudflare Worker (TypeScript, in graywolf-maps). Kubernetes CronJob. Svelte 5 + MapLibre GL JS (graywolf `web/`).

---

## What changed from the previous draft (read first)

This revision makes two sets of changes to the plan reviewed on 2026-06-12.

**1. v1 field source is now Level II super-res, per-site — not MRMS.** Per owner direction, the pipeline ships with **Level II** as the v1 default field source: per-site Archive II, 250 m gates, composited across the sites overlapping the bbox. This is the source that delivers the hook echoes, tight gradients and spiky cores that make the imagery gorgeous — MRMS is a pre-made ~1 km mosaic that smooths that structure away. The swappable `ReflectivityField` trait is unchanged in spirit; what was Tier 3 ("v2 fast-follow") is now the **v1 implementor**, and MRMS (Tier 2) + N0Q (Tier 1) are demoted to **fallback implementors behind the same trait**. Everything downstream of the `FieldGrid` — smoothing, contouring, Chaikin, tiling, MVT, packaging, publish, the entire client — is **identical** regardless of source, which is exactly why the trait was introduced. The new field-source work is Tasks 4–7 (site catalog, Level II decode, polar→Cartesian gridding, multi-site composite).

**2. All five High/Medium review issues are folded in.** Each is resolved at its task with a test:

| # | Severity | Issue | Resolved in |
|---|---|---|---|
| 1 | High | Sync `ReflectivityField` trait can't drive async S3 → runtime panic | **Task 3** — trait is now `async` (native async-fn-in-trait / `#[async_trait]`); `main` awaits. Also unlocks concurrent multi-site fetch (Task 7). |
| 2 | High | MRMS longitudes are 0–360, placed off-map | **Task 8** — MRMS fallback decoder normalizes `lon > 180 → lon − 360` with a CONUS-range test. (Moot for Level II, which is per-site polar — see Task 6 for the analogous Level II geolocation-correctness test.) |
| 3 | Medium | `build_pyramid` does boolean ∩ of every band × every tile, no pre-filter | **Task 14** — AABB overlap pre-filter before `.intersection()`, per-zoom Douglas–Peucker simplification, explicit early perf-budget check on a real frame. |
| 4 | Medium | `read_latest` swallows all errors as `None` → republish storms + orphans | **Task 17** — distinguish `NoSuchKey` (→ `None`) from transient errors (→ propagate / fail loudly). |
| 5 | Medium | MVT ring winding wrong after the y-flip → dropped fills | **Task 15** — take geozero's `MvtWriter` (handles command encoding + winding), with an explicit winding-repair fallback if the hand-rolled path is used. |

The Low/nits from the review (half-cell registration, blur re-mask comment, `lats[cols]` assumption) are noted inline where relevant but, per request, are not gated work.

**Honesty notes (unchanged):**

- **The "shipped raster PoC" doesn't exist in this repo.** Verified across all branches (`git log --all`, grep for radar/nexrad/dbz/contour — empty). The only map layers are `stations`, `trails`, `weather`, `my-position`, `hover-path`, plus sources `gw-federated-protocol`/`osm-raster`. The spec's `web/src/lib/map/layers/radar.js` and `radar-source.js`/`DBZ_COLORS` are **not** here. This plan **creates** the client layer and the NWS palette from scratch. If a PoC lives in `graywolf-maps`, reconcile the palette there.
- **Generator placement (resolves the open question): this repo, as a new `radar-contour/` Cargo workspace member.** The workspace, cross-rs build tooling and CI already live here; the generator shares no code with the Worker; its only coupling to graywolf-maps is the R2 bucket + tile-path convention (config, not code). The Worker route and the CronJob manifest still land in graywolf-maps.
- **Crate-boundary honesty.** The Rust pipeline depends on external crates that can't be compiled against in this workspace (`nexrad-*`, `gribberish`, `contour-isobands`, `geo` boolean ops, `geozero` MVT, `pmtiles` CLI). Where a task crosses a crate boundary, the exact data contract and the function we rely on are named, plus a `cargo build`/round-trip verification step, rather than fabricating a call that may be wrong. All of *our* logic (site geolocation, gridding/compositing, Gaussian, Chaikin, mercator, tiling, MBTiles schema, publish, the entire client) is complete, real code.

---

# Scope & Cross-Repo Layout

This plan covers **v1**: Level II super-res field source composited over the configured bbox (CONUS by default), vector isobands, PMTiles-per-frame packaging, z3–z10. The MRMS and N0Q sources ship as **fallback implementors** behind the trait (useful when Level II is unavailable, or for a fast/cheap CONUS mosaic), but Level II is the default.

Work spans **two repositories**:

| Repo | In this workspace? | What lands here |
|---|---|---|
| `graywolf` (this repo) | yes (checked out) | The Rust generator crate `radar-contour/` (Tasks 1–18) and the Svelte client layer (Tasks 22–26) |
| `graywolf-maps` (`~/dev/graywolf-maps`) | **no** — not checked out here | The origin Worker radar route (Tasks 19–20) and the k8s CronJob manifest (Task 21) |

**Placement decision (resolves the spec's open "generator placement").** The Rust generator goes in **this repo** as a new Cargo workspace member `radar-contour/`. Rationale: the Cargo workspace, cross-build tooling (cross-rs Docker mount of repo root), and CI already live here; the generator shares nothing with the Worker's TypeScript code; its only coupling to graywolf-maps is the R2 bucket name + the tile path convention, which are config. The Worker route and the CronJob manifest still land in graywolf-maps because that is where the R2/Worker/deploy surface lives (see `graywolf/docs/wiki/system-topology.md`). When you reach Tasks 19–21, check out graywolf-maps separately and follow its `.context/graywolf-client-integration.md`.

---

## File Structure

**New — `graywolf` repo (Rust generator):**

```
radar-contour/
  Cargo.toml                      # new workspace member
  src/
    main.rs                       # CLI entry: parse config, run one frame, exit
    config.rs                     # env/flag config (bucket, endpoint, bbox, zooms, sigma, grid res...)
    field/
      mod.rs                      # async ReflectivityField trait + FieldGrid type + GridSpec
      level2/
        mod.rs                    # Level2Source (v1) — async ReflectivityField impl
        sites.rs                  # NEXRAD site catalog (id, lat, lon, elev)
        decode.rs                 # Archive II super-res reflectivity sweep -> polar gates
        grid.rs                   # polar gate geolocation + max-composite onto FieldGrid
      mrms.rs                     # Tier-2 MRMS GRIB2 fallback (gribberish)
      n0q.rs                      # Tier-1 IEM N0Q PNG fallback (palette LUT)
    smooth.rs                     # separable Gaussian blur over FieldGrid
    contour.rs                    # filled isobands per dBZ threshold (contour-isobands)
    chaikin.rs                    # Chaikin corner-cutting on polygon rings
    mercator.rs                   # lon/lat <-> Web Mercator + tile bbox math
    tile.rs                       # clip bands per tile (AABB pre-filter + simplify), build pyramid
    mvt.rs                        # encode a tile's features to MVT bytes (geozero MvtWriter)
    package.rs                    # stage tiles to MBTiles, convert to PMTiles
    publish.rs                    # R2 put archive + flip latest.json; change-detect
  tests/
    fixtures/                     # tiny Level II + GRIB2 + PNG fixtures, golden values
```

Root `Cargo.toml` `members` gains `"radar-contour"`.

**New — `graywolf-maps` repo (not in this workspace):**

```
src/radar.ts                      # Worker handler: latest.json + radar tile range-serve
packaging/k8s/radar-contour-cronjob.yaml   # CronJob on big-bulky-1
```

**New — `graywolf` repo (client):**

```
web/src/lib/map/sources/radar-source.js    # DBZ_COLORS, DBZ_THRESHOLDS, source/layer specs
web/src/lib/map/layers/radar.js            # mountRadarLayer(): vector source + fill layers
```

**Modified — `graywolf` repo (client):**

```
web/src/routes/LiveMapV2.svelte            # mount radar layer, toggle + opacity slider
```

---

## Phase A — Rust generator (this repo)

### Task 1: Scaffold the `radar-contour` workspace member

**Files:**
- Create: `radar-contour/Cargo.toml`
- Create: `radar-contour/src/main.rs`
- Modify: `Cargo.toml` (root workspace `members`)

- [ ] **Step 1: Add the crate to the workspace**

In root `Cargo.toml`:

```toml
[workspace]
members = ["graywolf-modem", "radar-contour"]
resolver = "2"
```

- [ ] **Step 2: Write `radar-contour/Cargo.toml`**

```toml
[package]
name = "radar-contour"
version = "0.1.0"
edition = "2021"
description = "Level II reflectivity -> smoothed vector isoband PMTiles generator"

[[bin]]
name = "radar-contour"
path = "src/main.rs"

[lib]
name = "radar_contour"
path = "src/lib.rs"

[dependencies]
anyhow = "1"
clap = { version = "4", features = ["derive", "env"] }
tracing = "0.1"
tracing-subscriber = { version = "0.3", features = ["env-filter"] }
# Level II (v1 field source)
nexrad-data = "0.4"
nexrad-decode = "0.4"
nexrad-model = "0.4"
# Fallback field sources
gribberish = "0.20"
image = { version = "0.25", default-features = false, features = ["png"] }
# Pipeline
contour-isobands = "0.4"
geo = "0.28"
geo-types = "0.7"
geozero = { version = "0.14", features = ["with-mvt"] }
rusqlite = { version = "0.31", features = ["bundled"] }
# IO
aws-config = { version = "1", features = ["behavior-version-latest"] }
aws-sdk-s3 = "1"
tokio = { version = "1", features = ["macros", "rt-multi-thread"] }
async-trait = "0.1"
futures = "0.3"
serde = { version = "1", features = ["derive"] }
serde_json = "1"
flate2 = "1"

[dev-dependencies]
approx = "0.5"
```

> Crate versions are starting points. After Step 3, `cargo build` is the source of truth — if a crate's published API differs from what a later task assumes, adapt the call site; the task notes name the function we rely on so you know what to look for. The `nexrad-*` crate family (`nexrad-data` for AWS access, `nexrad-decode`/`nexrad-model` for Archive II) is the de-facto pure-Rust NEXRAD stack; confirm the exact version split at build time — some releases fold decode + model into one crate.

- [ ] **Step 3: Write a minimal `lib.rs` + `main.rs` that compile**

`radar-contour/src/lib.rs`:

```rust
pub mod config;
pub mod field;
pub mod smooth;
pub mod contour;
pub mod chaikin;
pub mod mercator;
pub mod tile;
pub mod mvt;
pub mod package;
pub mod publish;
```

`radar-contour/src/main.rs`:

```rust
use anyhow::Result;

#[tokio::main]
async fn main() -> Result<()> {
    tracing_subscriber::fmt().with_env_filter("info").init();
    tracing::info!("radar-contour starting");
    Ok(())
}
```

(Create empty stub modules as each task introduces them; for Step 4 a stub `config.rs`/`field/mod.rs`/etc. with `// implemented in Task N` is fine to make `lib.rs` resolve, or add the `pub mod` lines as you reach each task.)

- [ ] **Step 4: Verify the workspace builds**

Run: `cargo build -p radar-contour`
Expected: compiles, produces `target/debug/radar-contour`.

- [ ] **Step 5: Commit**

```bash
git add Cargo.toml radar-contour/
git commit -m "radar-contour: scaffold isoband tile generator crate"
```

---

### Task 2: Config struct

**Files:**
- Create: `radar-contour/src/config.rs`
- Modify: `radar-contour/src/main.rs`

The config adds a **grid resolution** (`grid_deg`) — the cell size of the composited lon/lat field — and the zoom range bumps to **z3–z10** to carry Level II's sharper structure.

- [ ] **Step 1: Write the failing test**

Create `radar-contour/src/config.rs`:

```rust
#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn defaults_cover_conus_z3_z10() {
        let c = Config::conus_defaults();
        assert_eq!(c.min_zoom, 3);
        assert_eq!(c.max_zoom, 10);
        assert!(c.bbox.west < c.bbox.east);
        assert!(c.bbox.south < c.bbox.north);
        assert!(c.gaussian_sigma > 0.0);
        assert!(c.chaikin_iterations >= 1);
        assert!(c.grid_deg > 0.0 && c.grid_deg < 0.1);
        assert_eq!(c.dbz_thresholds.first().copied(), Some(5.0));
    }
}
```

- [ ] **Step 2: Run it (fails to compile — `Config` undefined)**

Run: `cargo test -p radar-contour config::tests::defaults_cover_conus_z3_z10`
Expected: FAIL — `cannot find type Config`.

- [ ] **Step 3: Implement `Config`**

Prepend to `radar-contour/src/config.rs`:

```rust
use clap::Parser;

#[derive(Debug, Clone, Copy)]
pub struct BBox {
    pub west: f64,
    pub south: f64,
    pub east: f64,
    pub north: f64,
}

#[derive(Debug, Clone, Parser)]
#[command(about = "Level II reflectivity -> vector isoband PMTiles")]
pub struct Config {
    /// R2 bucket name
    #[arg(long, env = "RADAR_R2_BUCKET")]
    pub r2_bucket: Option<String>,
    /// R2 S3-compatible endpoint URL
    #[arg(long, env = "RADAR_R2_ENDPOINT")]
    pub r2_endpoint: Option<String>,
    /// Object-key prefix under the bucket
    #[arg(long, env = "RADAR_R2_PREFIX", default_value = "radar")]
    pub r2_prefix: String,
    #[arg(long, default_value_t = 3)]
    pub min_zoom: u8,
    #[arg(long, default_value_t = 10)]
    pub max_zoom: u8,
    #[arg(long, default_value_t = 1.0)]
    pub gaussian_sigma: f64,
    #[arg(long, default_value_t = 2)]
    pub chaikin_iterations: u8,
    /// Composite grid cell size in degrees (~0.0025deg ~= 250 m at mid-lat).
    #[arg(long, default_value_t = 0.0025)]
    pub grid_deg: f64,

    #[clap(skip = Config::default_bbox())]
    pub bbox: BBox,
    #[clap(skip = Config::default_thresholds())]
    pub dbz_thresholds: Vec<f64>,
}

impl Config {
    fn default_bbox() -> BBox {
        // CONUS, generous margins.
        BBox { west: -127.0, south: 20.0, east: -65.0, north: 51.0 }
    }
    fn default_thresholds() -> Vec<f64> {
        // NWS reflectivity breakpoints, 5..=75 by 5.
        (1..=15).map(|i| (i * 5) as f64).collect()
    }
    pub fn conus_defaults() -> Self {
        Self {
            r2_bucket: None,
            r2_endpoint: None,
            r2_prefix: "radar".into(),
            min_zoom: 3,
            max_zoom: 10,
            gaussian_sigma: 1.0,
            chaikin_iterations: 2,
            grid_deg: 0.0025,
            bbox: Self::default_bbox(),
            dbz_thresholds: Self::default_thresholds(),
        }
    }
}
```

> **Perf note (ties to Task 14, review #3).** `grid_deg = 0.0025` over full CONUS is ~24800 × 12400 ≈ 3.1e8 cells — that is the *maximum* fidelity Level II can express, and it is heavy to contour every cycle. Task 14 includes an explicit perf-budget check on `big-bulky-1`; the two levers if it overruns the cadence are (a) raise `grid_deg` (coarser composite — still Level-II-sourced, just binned larger) and (b) **narrow `bbox` to a region**. v1 default is CONUS; treat the perf check as a gate before wiring the CronJob, not an afterthought.

- [ ] **Step 4: Run the test (passes)**

Run: `cargo test -p radar-contour config::tests::defaults_cover_conus_z3_z10`
Expected: PASS.

- [ ] **Step 5: Wire `config` into `main.rs` and commit**

In `main.rs` add `use radar_contour::config::Config;` and `let cfg = Config::parse();` (then `tracing::info!(cfg.min_zoom, cfg.max_zoom, "config loaded");`).

```bash
cargo build -p radar-contour
git add radar-contour/
git commit -m "radar-contour: config struct with CONUS defaults + grid resolution"
```

---

### Task 3: `FieldGrid` + `GridSpec` + the **async** `ReflectivityField` trait

> **Resolves review High #1.** The trait is async so its S3-backed implementors `.await` their list/get calls under the live tokio reactor instead of `block_on`-ing inside a sync method (which panics: "Cannot start a runtime from within a runtime"). This also lets the Level II source fetch many sites concurrently (Task 7).

**Files:**
- Create: `radar-contour/src/field/mod.rs`

This task also introduces `GridSpec` — the regular lon/lat lattice the Level II compositor writes into and the rest of the pipeline reads. (For the fallback sources, the decoder produces its own grid; `GridSpec` is the *target* lattice for compositing many polar sources.)

- [ ] **Step 1: Write the failing test**

Create `radar-contour/src/field/mod.rs`:

```rust
#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn grid_indexes_row_major_and_reports_geo() {
        // 2 cols x 2 rows, origin at (lon0,lat0), 1deg cells.
        let g = FieldGrid {
            cols: 2, rows: 2,
            lon0: -100.0, lat0: 40.0, dlon: 1.0, dlat: -1.0,
            values: vec![0.0, 1.0, 2.0, 3.0],
        };
        assert_eq!(g.at(0, 0), 0.0);
        assert_eq!(g.at(1, 0), 1.0); // col 1, row 0
        assert_eq!(g.at(0, 1), 2.0); // col 0, row 1
        let (lon, lat) = g.cell_center(1, 1);
        assert!((lon - (-98.5)).abs() < 1e-9);
        assert!((lat - 38.5).abs() < 1e-9);
    }

    #[test]
    fn gridspec_maps_lonlat_to_nearest_cell() {
        let spec = GridSpec::from_bbox(
            &crate::config::BBox { west: -100.0, south: 30.0, east: -90.0, north: 40.0 }, 1.0);
        assert_eq!(spec.cols, 10);
        assert_eq!(spec.rows, 10);
        // A point just inside the NW corner lands at (col 0, row 0).
        assert_eq!(spec.cell_index(-99.9, 39.9), Some((0, 0)));
        // Out of bounds -> None.
        assert_eq!(spec.cell_index(-80.0, 39.0), None);
    }
}
```

- [ ] **Step 2: Run it (fails)**

Run: `cargo test -p radar-contour field::tests`
Expected: FAIL — `FieldGrid`/`GridSpec` undefined.

- [ ] **Step 3: Implement `FieldGrid`, `GridSpec`, and the async trait**

Prepend to `radar-contour/src/field/mod.rs`:

```rust
use anyhow::Result;
use crate::config::BBox;

pub mod level2;
pub mod mrms;
pub mod n0q;

/// A dBZ field on a regular lon/lat grid, row-major from the NW corner.
/// `dlat` is typically negative (rows march south). Missing data is encoded
/// as f64::NAN.
#[derive(Debug, Clone)]
pub struct FieldGrid {
    pub cols: usize,
    pub rows: usize,
    pub lon0: f64,
    pub lat0: f64,
    pub dlon: f64,
    pub dlat: f64,
    pub values: Vec<f64>, // len == cols*rows
}

impl FieldGrid {
    pub fn at(&self, col: usize, row: usize) -> f64 {
        self.values[row * self.cols + col]
    }
    /// Geographic center of a cell.
    pub fn cell_center(&self, col: usize, row: usize) -> (f64, f64) {
        let lon = self.lon0 + (col as f64 + 0.5) * self.dlon;
        let lat = self.lat0 + (row as f64 + 0.5) * self.dlat;
        (lon, lat)
    }
}

/// Target lattice for compositing polar sources. North-up: row 0 is the
/// north edge, dlat negative. `lon0`/`lat0` are the NW node (corner) coords.
#[derive(Debug, Clone, Copy)]
pub struct GridSpec {
    pub cols: usize,
    pub rows: usize,
    pub lon0: f64,
    pub lat0: f64,
    pub dlon: f64, // = grid_deg
    pub dlat: f64, // = -grid_deg
}

impl GridSpec {
    pub fn from_bbox(bbox: &BBox, grid_deg: f64) -> Self {
        let cols = (((bbox.east - bbox.west) / grid_deg).round() as usize).max(1);
        let rows = (((bbox.north - bbox.south) / grid_deg).round() as usize).max(1);
        GridSpec {
            cols, rows,
            lon0: bbox.west,
            lat0: bbox.north,
            dlon: grid_deg,
            dlat: -grid_deg,
        }
    }
    /// Nearest (col, row) for a lon/lat, or None if outside the lattice.
    pub fn cell_index(&self, lon: f64, lat: f64) -> Option<(usize, usize)> {
        let fx = (lon - self.lon0) / self.dlon;
        let fy = (lat - self.lat0) / self.dlat; // dlat negative -> grows south
        if fx < 0.0 || fy < 0.0 { return None; }
        let (cx, cy) = (fx.floor() as usize, fy.floor() as usize);
        if cx >= self.cols || cy >= self.rows { return None; }
        Some((cx, cy))
    }
    /// An all-NaN FieldGrid sized to this spec, ready for max-compositing.
    pub fn empty_field(&self) -> FieldGrid {
        FieldGrid {
            cols: self.cols, rows: self.rows,
            lon0: self.lon0, lat0: self.lat0,
            dlon: self.dlon, dlat: self.dlat,
            values: vec![f64::NAN; self.cols * self.rows],
        }
    }
}

/// Swappable field source. v1 ships Level II (per-site super-res) as the
/// default; MRMS + N0Q remain as fallback implementors behind this trait.
/// Async because every implementor is backed by anonymous S3/HTTP reads.
#[async_trait::async_trait]
pub trait ReflectivityField {
    /// Identifier of the latest available frame (e.g. a cadence-rounded
    /// RFC3339 timestamp). Used for change detection.
    async fn latest_frame_id(&self) -> Result<String>;
    /// Fetch + decode + (for Level II) composite the field for a frame id.
    async fn fetch(&self, frame_id: &str) -> Result<FieldGrid>;
}
```

> `#[async_trait]` keeps the trait object-safe and `Send`-friendly across tokio worker threads; native `async fn` in traits (stable since 1.75) also works if you don't need `dyn ReflectivityField`. We use a concrete `Level2Source` in `main`, so either is fine — `#[async_trait]` is chosen for portability.

- [ ] **Step 4: Stub the source modules so the crate compiles**

Create `radar-contour/src/field/level2/mod.rs` with `pub mod sites; pub mod decode; pub mod grid;` and empty stub files for each (`// implemented in Task N`). Create empty `radar-contour/src/field/mrms.rs` and `radar-contour/src/field/n0q.rs` stubs.

- [ ] **Step 5: Run the test (passes) and commit**

Run: `cargo test -p radar-contour field::tests`
Expected: PASS.

```bash
git add radar-contour/
git commit -m "radar-contour: FieldGrid, GridSpec, async ReflectivityField trait"
```

---

### Task 4: NEXRAD site catalog

**Files:**
- Create: `radar-contour/src/field/level2/sites.rs`

The compositor needs each WSR-88D site's `(id, lat, lon, elev_m)` to geolocate its gates. Embed the static catalog (≈160 CONUS sites) as a `const` table and expose a bbox filter so we only fetch/decode sites whose ~230 km coverage disc overlaps the configured bbox.

- [ ] **Step 1: Write the failing test**

Create `radar-contour/src/field/level2/sites.rs`:

```rust
#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn catalog_has_known_sites_and_filters_by_bbox() {
        let all = sites();
        // A few canonical sites must be present.
        assert!(all.iter().any(|s| s.id == "KTLX")); // Oklahoma City
        assert!(all.iter().any(|s| s.id == "KFWS")); // Dallas/Fort Worth
        // Filtering to a small box around KTLX returns KTLX, not far-away sites.
        let bbox = crate::config::BBox { west:-98.5, south:35.0, east:-97.0, north:36.0 };
        let near = sites_overlapping(&bbox, 230_000.0);
        assert!(near.iter().any(|s| s.id == "KTLX"));
        assert!(!near.iter().any(|s| s.id == "KMUX")); // SF Bay Area, far away
    }
}
```

- [ ] **Step 2: Run it (fails)**

Run: `cargo test -p radar-contour field::level2::sites::tests`
Expected: FAIL — `sites`/`sites_overlapping` undefined.

- [ ] **Step 3: Implement the catalog + filter**

Prepend to `radar-contour/src/field/level2/sites.rs`:

```rust
use crate::config::BBox;

#[derive(Debug, Clone, Copy)]
pub struct Site {
    pub id: &'static str,
    pub lat: f64,
    pub lon: f64,
    pub elev_m: f64,
}

/// WSR-88D site catalog. Source of truth: NWS/ROC station list
/// (`https://www.roc.noaa.gov/.../Site_List.csv`) — embed the full CONUS set.
/// Truncated here; the implementor pastes the complete table.
const SITES: &[Site] = &[
    Site { id: "KTLX", lat: 35.3331, lon: -97.2778, elev_m: 370.0 },
    Site { id: "KFWS", lat: 32.5731, lon: -97.3031, elev_m: 208.0 },
    Site { id: "KMUX", lat: 37.1551, lon: -121.8983, elev_m: 1057.0 },
    // ... full CONUS catalog (~160 rows) ...
];

pub fn sites() -> &'static [Site] { SITES }

/// Approx great-circle distance (m) on a sphere.
fn haversine_m(lat1: f64, lon1: f64, lat2: f64, lon2: f64) -> f64 {
    const R: f64 = 6_371_000.0;
    let (p1, p2) = (lat1.to_radians(), lat2.to_radians());
    let dp = (lat2 - lat1).to_radians();
    let dl = (lon2 - lon1).to_radians();
    let a = (dp / 2.0).sin().powi(2) + p1.cos() * p2.cos() * (dl / 2.0).sin().powi(2);
    2.0 * R * a.sqrt().asin()
}

/// Sites whose `range_m` coverage disc overlaps the bbox (cheap: test the
/// bbox center + corners against the site, padded by range).
pub fn sites_overlapping(bbox: &BBox, range_m: f64) -> Vec<Site> {
    let cx = (bbox.west + bbox.east) / 2.0;
    let cy = (bbox.south + bbox.north) / 2.0;
    // bbox half-diagonal in meters (rough), so disc∩bbox <=> site within (range + halfdiag) of center.
    let half_diag = haversine_m(bbox.south, bbox.west, bbox.north, bbox.east) / 2.0;
    SITES.iter().cloned()
        .filter(|s| haversine_m(s.lat, s.lon, cy, cx) <= range_m + half_diag)
        .collect()
}
```

> The 3 rows shown are real; paste the complete ROC catalog when implementing. The overlap test is intentionally conservative (center-distance vs range + half-diagonal) — it errs toward *including* a site, which is correct (a missed site leaves a gap; an extra site just adds work the bbox clip discards). Tighten later if site count over CONUS is too high.

- [ ] **Step 4: Run the test (passes) and commit**

Run: `cargo test -p radar-contour field::level2::sites::tests`
Expected: PASS.

```bash
git add radar-contour/
git commit -m "radar-contour: NEXRAD site catalog + bbox overlap filter"
```

---

### Task 5: Level II super-res decode (per-site polar reflectivity)

**Files:**
- Create: `radar-contour/src/field/level2/decode.rs`
- Test: `radar-contour/tests/level2_decode.rs`
- Create fixture: `radar-contour/tests/fixtures/level2_tiny` (one real volume chunk)

Decode one site's Archive II volume to the **lowest-tilt super-res reflectivity sweep**: a set of radials, each with an azimuth (deg, clockwise from true north), an elevation angle, a first-gate range + gate spacing (250 m super-res), and a `Vec<f64>` of dBZ per gate (with below-threshold/missing → NaN).

- [ ] **Step 1: Create a fixture**

Download one real Level II volume (or a chunk) for a site and keep a trimmed copy:

```bash
# nexrad-data exposes the unidata-nexrad-level2 real-time + archive buckets;
# alternatively grab one directly:
aws s3 ls --no-sign-request s3://unidata-nexrad-level2/<YYYY>/<MM>/<DD>/KTLX/ | tail
aws s3 cp --no-sign-request \
  s3://unidata-nexrad-level2/<YYYY>/<MM>/<DD>/KTLX/<KTLX_..._V06> \
  radar-contour/tests/fixtures/level2_tiny
```

- [ ] **Step 2: Write the failing test**

Create `radar-contour/tests/level2_decode.rs`:

```rust
use radar_contour::field::level2::decode::decode_reflectivity_sweep;

#[test]
fn decodes_lowest_tilt_reflectivity() {
    let bytes = std::fs::read("tests/fixtures/level2_tiny").unwrap();
    let sweep = decode_reflectivity_sweep(&bytes).expect("decode");
    // Super-res: ~720 radials (0.5deg) and 250 m gates.
    assert!(sweep.radials.len() > 300);
    assert!(sweep.gate_spacing_m > 0.0 && sweep.gate_spacing_m <= 300.0);
    // Lowest tilt ~0.5deg.
    assert!(sweep.radials[0].elevation_deg < 2.0);
    // Some real echo present and in a sane dBZ range.
    let any = sweep.radials.iter().flat_map(|r| r.gates.iter())
        .cloned().filter(|v| !v.is_nan());
    let max = any.fold(f64::MIN, f64::max);
    assert!(max <= 95.0 && max >= 0.0);
}
```

- [ ] **Step 3: Run it (fails)**

Run: `cargo test -p radar-contour --test level2_decode`
Expected: FAIL — `decode_reflectivity_sweep` undefined.

- [ ] **Step 4: Implement the decoder**

Replace `radar-contour/src/field/level2/decode.rs`:

```rust
use anyhow::{anyhow, Result};

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

/// Decode an Archive II volume's lowest-tilt reflectivity (REF moment) sweep
/// to polar radials. Uses nexrad-decode to parse message-31 radials, keeps
/// only the lowest elevation cut that carries reflectivity.
pub fn decode_reflectivity_sweep(bytes: &[u8]) -> Result<Sweep> {
    // nexrad-data/decode: parse the volume, iterate message-31 radials,
    // pull the REF moment block. Accessor names below are the contract; map
    // them to the installed crate's API (see note).
    let volume = nexrad_decode::decode_archive2(bytes)
        .map_err(|e| anyhow!("level2 decode: {e}"))?;

    let mut radials: Vec<Radial> = Vec::new();
    let mut min_elev = f64::MAX;

    for radial in volume.radials() {
        let Some(ref_moment) = radial.reflectivity() else { continue };
        let elev = radial.elevation_angle_deg();
        let az = radial.azimuth_angle_deg();
        let first_gate = ref_moment.first_gate_range_m();
        let spacing = ref_moment.gate_spacing_m();

        // Moment values arrive as scaled codes; the crate yields dBZ with a
        // missing/below-threshold sentinel. Map sentinels to NaN.
        let gates: Vec<f64> = ref_moment.values_dbz().into_iter()
            .map(|v| match v { Some(d) => d, None => f64::NAN })
            .collect();

        min_elev = min_elev.min(elev);
        radials.push(Radial {
            azimuth_deg: az,
            elevation_deg: elev,
            first_gate_m: first_gate,
            gate_spacing_m: spacing,
            gates,
        });
    }
    if radials.is_empty() {
        return Err(anyhow!("no reflectivity radials in volume"));
    }
    // Keep only the lowest cut (super-res 0.5deg reflectivity scan).
    let tol = 0.25;
    radials.retain(|r| (r.elevation_deg - min_elev).abs() <= tol);
    let gate_spacing_m = radials[0].gate_spacing_m;
    Ok(Sweep { gate_spacing_m, radials })
}
```

> Confirm `nexrad-decode`/`nexrad-model` accessor names against the installed version — the message-31 model has shifted across releases (`radials()`, `reflectivity()`/moment block, `azimuth_angle_deg`, `elevation_angle_deg`, `first_gate_range_m`, `gate_spacing_m`, scaled-code → dBZ conversion). The contract this task guarantees downstream: a `Sweep` of `Radial`s with azimuth (deg CW from N), elevation, gate geometry (m), and dBZ-or-NaN per gate. If the crate exposes a higher-level `Scan`/`Sweep` model that already yields `(azimuth, range, dbz)` triples, use it and skip the manual moment unpacking.

- [ ] **Step 5: Run the test (passes) and commit**

Run: `cargo test -p radar-contour --test level2_decode`
Expected: PASS.

```bash
git add radar-contour/
git commit -m "radar-contour: Level II super-res reflectivity sweep decode"
```

---

### Task 6: Polar gate geolocation + single-site rasterization

> **Replaces review High #2 for the v1 path.** Level II is per-site polar, so there is no 0–360 longitude grid to normalize; the analogous correctness risk is **gate geolocation** (azimuth convention + slant→ground range). This task pins both with a test asserting a gate lands at the geographically correct spot.

**Files:**
- Create: `radar-contour/src/field/level2/grid.rs`

For each gate, compute its ground lon/lat from the site location, the radial azimuth, and the slant range (corrected to ground range), then write its dBZ into the nearest `GridSpec` cell with a **max** combine. Multiple sites accumulate into the same `FieldGrid` (Task 7).

- [ ] **Step 1: Write the failing test**

Create `radar-contour/src/field/level2/grid.rs`:

```rust
#[cfg(test)]
mod tests {
    use super::*;
    use crate::field::GridSpec;
    use crate::field::level2::decode::{Radial, Sweep};
    use crate::field::level2::sites::Site;

    fn site() -> Site { Site { id: "KTST", lat: 35.0, lon: -97.0, elev_m: 0.0 } }

    #[test]
    fn gate_due_east_lands_east_of_site() {
        // One radial, azimuth 90deg (due east), a single 50 dBZ gate at ~50 km.
        let sweep = Sweep {
            gate_spacing_m: 250.0,
            radials: vec![Radial {
                azimuth_deg: 90.0, elevation_deg: 0.5,
                first_gate_m: 50_000.0, gate_spacing_m: 250.0,
                gates: vec![50.0],
            }],
        };
        let spec = GridSpec::from_bbox(
            &crate::config::BBox { west:-98.0, south:34.0, east:-96.0, north:36.0 }, 0.01);
        let mut field = spec.empty_field();
        composite_site(&mut field, &spec, &site(), &sweep);
        // Find the populated cell; it must be EAST (lon > -97) and near lat 35.
        let mut hit = None;
        for row in 0..spec.rows {
            for col in 0..spec.cols {
                if field.at(col, row) == 50.0 { hit = Some(field.cell_center(col, row)); }
            }
        }
        let (lon, lat) = hit.expect("a populated cell");
        assert!(lon > -97.0, "gate must be east of site, got lon {lon}");
        assert!((lat - 35.0).abs() < 0.2, "due-east gate stays near site lat");
    }

    #[test]
    fn max_combine_keeps_stronger_echo() {
        let spec = GridSpec::from_bbox(
            &crate::config::BBox { west:-98.0, south:34.0, east:-96.0, north:36.0 }, 0.01);
        let mut field = spec.empty_field();
        let mk = |dbz: f64| Sweep { gate_spacing_m: 250.0, radials: vec![Radial {
            azimuth_deg: 90.0, elevation_deg: 0.5, first_gate_m: 50_000.0,
            gate_spacing_m: 250.0, gates: vec![dbz] }] };
        composite_site(&mut field, &spec, &site(), &mk(30.0));
        composite_site(&mut field, &spec, &site(), &mk(45.0));
        composite_site(&mut field, &spec, &site(), &mk(40.0));
        let max = field.values.iter().cloned().filter(|v| !v.is_nan()).fold(f64::MIN, f64::max);
        assert_eq!(max, 45.0); // strongest echo wins
    }
}
```

- [ ] **Step 2: Run it (fails)**

Run: `cargo test -p radar-contour field::level2::grid::tests`
Expected: FAIL — `composite_site` undefined.

- [ ] **Step 3: Implement geolocation + rasterization**

Prepend to `radar-contour/src/field/level2/grid.rs`:

```rust
use crate::field::{FieldGrid, GridSpec};
use crate::field::level2::decode::Sweep;
use crate::field::level2::sites::Site;

const EARTH_R: f64 = 6_371_000.0;
const KE: f64 = 4.0 / 3.0; // effective-earth-radius beam model

/// Ground (great-circle) range for a slant range at a beam elevation, using
/// the standard 4/3-earth approximation. At <=230 km this is within a few
/// hundred meters of the rigorous value — well under one 250 m gate.
fn ground_range_m(slant_m: f64, elev_deg: f64) -> f64 {
    let ae = KE * EARTH_R;
    let el = elev_deg.to_radians();
    // s = ae * atan( r*cos(el) / (ae + r*sin(el)) )
    ae * ((slant_m * el.cos()) / (ae + slant_m * el.sin())).atan()
}

/// Destination lon/lat from an origin, a bearing (deg CW from true north),
/// and a ground distance — spherical direct (haversine) formula.
fn dest_lonlat(lat0: f64, lon0: f64, bearing_deg: f64, dist_m: f64) -> (f64, f64) {
    let ang = dist_m / EARTH_R;
    let br = bearing_deg.to_radians();
    let (p0, l0) = (lat0.to_radians(), lon0.to_radians());
    let lat = (p0.sin() * ang.cos() + p0.cos() * ang.sin() * br.cos()).asin();
    let lon = l0 + (br.sin() * ang.sin() * p0.cos())
        .atan2(ang.cos() - p0.sin() * lat.sin());
    (lon.to_degrees(), lat.to_degrees())
}

/// Geolocate every gate of `sweep` from `site` and max-combine its dBZ into
/// `field` at the nearest `spec` cell. NaN gates are skipped.
pub fn composite_site(field: &mut FieldGrid, spec: &GridSpec, site: &Site, sweep: &Sweep) {
    for radial in &sweep.radials {
        for (i, &dbz) in radial.gates.iter().enumerate() {
            if dbz.is_nan() { continue; }
            let slant = radial.first_gate_m + i as f64 * radial.gate_spacing_m;
            let ground = ground_range_m(slant, radial.elevation_deg);
            let (lon, lat) = dest_lonlat(site.lat, site.lon, radial.azimuth_deg, ground);
            if let Some((col, row)) = spec.cell_index(lon, lat) {
                let idx = row * spec.cols + col;
                let cur = field.values[idx];
                field.values[idx] = if cur.is_nan() { dbz } else { cur.max(dbz) };
            }
        }
    }
}
```

> Geolocation correctness, locked by the test: azimuth is **clockwise from true north** (bearing convention used by `dest_lonlat`), and slant range is converted to **ground range** before placement. A flat-earth `dx = r·sin(az), dy = r·cos(az)` shortcut is accurate enough at radar scales too, but the spherical direct formula above costs nothing and stays correct at the bbox edges where many sites overlap. Gaps between gates at far range (gate footprint > cell) can leave pinholes; the Gaussian blur (Task 10) closes them. Note `nexrad` azimuths are already true-north CW — if the installed crate reports otherwise, adjust here, not downstream.

- [ ] **Step 4: Run the tests (pass)**

Run: `cargo test -p radar-contour field::level2::grid::tests`
Expected: PASS (both).

- [ ] **Step 5: Commit**

```bash
git add radar-contour/
git commit -m "radar-contour: Level II gate geolocation + single-site rasterize"
```

---

### Task 7: `Level2Source` — multi-site fetch + composite (v1 `ReflectivityField`)

**Files:**
- Modify: `radar-contour/src/field/level2/mod.rs`

Implements the async trait for the v1 source: select sites overlapping the bbox, discover each site's newest volume on `unidata-nexrad-level2`, **fetch them concurrently**, decode the lowest-tilt sweep, and `composite_site` each into one `FieldGrid`.

- [ ] **Step 1: Write the failing test (pure logic: frame-id rounding + empty composite)**

Append to `radar-contour/src/field/level2/mod.rs`:

```rust
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
            &crate::config::BBox { west:-98.0, south:34.0, east:-96.0, north:36.0 }, 0.05);
        let field = composite_sweeps(&spec, &[]);
        assert!(field.values.iter().all(|v| v.is_nan()));
    }
}
```

- [ ] **Step 2: Run it (fails)**

Run: `cargo test -p radar-contour field::level2::tests`
Expected: FAIL — `cadence_bucket`/`composite_sweeps` undefined.

- [ ] **Step 3: Implement the source**

Prepend to `radar-contour/src/field/level2/mod.rs` (above the `pub mod` lines if present, keeping them):

```rust
pub mod sites;
pub mod decode;
pub mod grid;

use crate::config::BBox;
use crate::field::{FieldGrid, GridSpec, ReflectivityField};
use anyhow::Result;
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
/// string/parse math so it is unit-testable without a clock.
pub fn cadence_bucket(rfc3339: &str, window_s: i64) -> String {
    // Parse with time/chrono in the real impl; here, conceptually:
    //   epoch = parse(rfc3339); bucket = epoch - (epoch % window_s); format(bucket)
    // Implementor: use `time::OffsetDateTime::parse` + Rfc3339, floor the
    // unix timestamp to window_s, reformat. Keep the assertion in the test
    // (01:07:43 / 300 -> 01:05:00) as the contract.
    radar_contour_time::floor_rfc3339(rfc3339, window_s)
}

/// Composite already-decoded (site, sweep) pairs into one FieldGrid.
pub fn composite_sweeps(spec: &GridSpec, sweeps: &[(Site, Sweep)]) -> FieldGrid {
    let mut field = spec.empty_field();
    for (site, sweep) in sweeps {
        grid::composite_site(&mut field, spec, site, sweep);
    }
    field
}

#[async_trait::async_trait]
impl ReflectivityField for Level2Source {
    async fn latest_frame_id(&self) -> Result<String> {
        // List the newest volume key per overlapping site; take the max
        // wall-clock time; round to cadence. nexrad-data exposes a real-time
        // listing for unidata-nexrad-level2 — use it, else list_objects_v2
        // under <YYYY>/<MM>/<DD>/<SITE>/ and take the lexically-greatest key.
        let chosen = sites::sites_overlapping(&self.bbox, COVERAGE_M);
        let newest = newest_volume_time(&chosen).await?; // RFC3339
        Ok(cadence_bucket(&newest, self.cadence_s))
    }

    async fn fetch(&self, _frame_id: &str) -> Result<FieldGrid> {
        let spec = GridSpec::from_bbox(&self.bbox, self.grid_deg);
        let chosen = sites::sites_overlapping(&self.bbox, COVERAGE_M);

        // Concurrent fetch+decode per site (this is why the trait is async —
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
```

Add the two async IO helpers (kept out of unit tests, exercised by the smoke test in Task 18):
- `newest_volume_time(sites) -> Result<String>` — for each site list its newest key, parse the embedded scan time, return the max as RFC3339.
- `fetch_and_decode_site(site) -> Result<Sweep>` — anonymous `get_object` (via `nexrad-data` or `aws-sdk-s3` with `--no-sign-request` creds) of the site's newest volume, then `decode::decode_reflectivity_sweep`.

> `radar_contour_time::floor_rfc3339` is a stand-in name for a 6-line helper (parse RFC3339 → unix → floor to `window_s` → reformat) using `time` or `chrono`; add `time = { version = "0.3", features = ["parsing","formatting"] }` and implement it in a small `src/timeutil.rs`, or inline it. The test (`01:07:43 / 300 → 01:05:00`) is the contract.

- [ ] **Step 4: Run the tests (pass)**

Run: `cargo test -p radar-contour field::level2::tests`
Expected: PASS (both).

- [ ] **Step 5: Commit**

```bash
git add radar-contour/
git commit -m "radar-contour: Level2Source - concurrent multi-site fetch + composite"
```

---

### Task 8: MRMS GRIB2 fallback decoder (with 0–360 longitude fix)

> **Resolves review High #2.** MRMS is now a fallback, but the decoder still ships, so the 0–360 longitude normalization is implemented here with a CONUS-range assertion.

**Files:**
- Modify: `radar-contour/src/field/mrms.rs`
- Test: `radar-contour/tests/mrms_decode.rs`
- Create fixture: `radar-contour/tests/fixtures/mrms_tiny.grib2`

- [ ] **Step 1: Create a fixture**

```bash
aws s3 ls --no-sign-request s3://noaa-mrms-pds/CONUS/MergedReflectivityQComposite_00.50/ | tail
aws s3 cp --no-sign-request \
  s3://noaa-mrms-pds/CONUS/MergedReflectivityQComposite_00.50/<latest>.grib2.gz \
  radar-contour/tests/fixtures/mrms_tiny.grib2.gz
gunzip radar-contour/tests/fixtures/mrms_tiny.grib2.gz
```

- [ ] **Step 2: Write the failing test (asserts the lon normalization)**

Create `radar-contour/tests/mrms_decode.rs`:

```rust
use radar_contour::field::mrms::decode_grib2;

#[test]
fn decodes_mrms_composite_to_conus_dbz_grid() {
    let bytes = std::fs::read("tests/fixtures/mrms_tiny.grib2").unwrap();
    let grid = decode_grib2(&bytes).expect("decode");
    assert!(grid.cols > 100 && grid.rows > 100);
    // review #2: longitudes normalized into -180..180; CONUS origin sane.
    assert!(grid.lon0 > -127.0 && grid.lon0 < -65.0, "lon0 in CONUS, got {}", grid.lon0);
    assert!(grid.values.iter().any(|v| v.is_nan())); // sentinels -> NaN
    let max = grid.values.iter().cloned().filter(|v| !v.is_nan()).fold(f64::MIN, f64::max);
    assert!(max <= 95.0 && max >= 0.0);
    assert!(grid.dlat < 0.0); // marches south
}
```

- [ ] **Step 3: Run it (fails)**

Run: `cargo test -p radar-contour --test mrms_decode`
Expected: FAIL — `decode_grib2` undefined.

- [ ] **Step 4: Implement the decoder with normalization**

Replace `radar-contour/src/field/mrms.rs`:

```rust
use crate::field::FieldGrid;
use anyhow::{anyhow, Result};
use gribberish::message::Message;

/// Decode a single-message MRMS GRIB2 composite-reflectivity field to dBZ.
/// Sentinels (-999 no coverage, -99 no echo, < -90) -> NaN. GRIB2 encodes
/// longitude in 0..360; we normalize every lon to -180..180 so CONUS lands
/// at ~-127..-65 instead of ~233..295 (review #2).
pub fn decode_grib2(bytes: &[u8]) -> Result<FieldGrid> {
    let messages = Message::parse_all(bytes);
    let msg = messages.into_iter().next().ok_or_else(|| anyhow!("no GRIB2 message"))?;

    let (rows, cols) = msg.grid_dimensions()?; // (ny, nx)
    let (lats, lons) = msg.latlng()?;          // flattened, row-major
    let raw = msg.data()?;                      // Vec<f64>, row-major

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
```

> `gribberish` accessor names (`grid_dimensions`, `latlng`, `data`, `Message::parse_all`) must be confirmed against the installed version — adapt names only; the mapping is the contract. The `dlon` is computed from normalized lons so a grid that wraps the 0/360 seam doesn't produce a spurious ~360° step (CONUS doesn't cross it, but the normalization is correct regardless).

- [ ] **Step 5: Run the test (passes) and commit**

Run: `cargo test -p radar-contour --test mrms_decode`
Expected: PASS.

```bash
git add radar-contour/
git commit -m "radar-contour: MRMS GRIB2 fallback decode with 0-360 lon fix"
```

---

### Task 9: N0Q PNG fallback source (Tier 1)

**Files:**
- Modify: `radar-contour/src/field/n0q.rs`
- Test: in-module `#[cfg(test)]`

The IEM N0Q national mosaic is a PNG whose palette is an invertible 0.5-dBZ LUT: RGB → dBZ by direct lookup. Build the inverse LUT once, then map every pixel. (Unchanged from the prior draft; included so all three tiers exist behind the trait.)

- [ ] **Step 1: Write the failing test**

Append to `radar-contour/src/field/n0q.rs`:

```rust
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
```

- [ ] **Step 2: Run it (fails)**

Run: `cargo test -p radar-contour field::n0q::tests::lut_roundtrips_known_palette_entries`
Expected: FAIL — `n0q_lut` undefined.

- [ ] **Step 3: Implement the LUT + decoder**

Prepend to `radar-contour/src/field/n0q.rs`:

```rust
use crate::field::FieldGrid;
use anyhow::Result;
use image::GenericImageView;

/// N0Q palette: index i (i>=1) -> dBZ = -32.0 + (i-1)*0.5; index 0 = NaN.
pub fn n0q_lut() -> [f64; 256] {
    let mut lut = [f64::NAN; 256];
    for i in 1..256usize { lut[i] = -32.0 + (i as f64 - 1.0) * 0.5; }
    lut
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

fn rgba_to_dbz(_rgba: [u8; 4], _lut: &[f64; 256]) -> f64 { f64::NAN } // finished in Step 4
```

- [ ] **Step 4: Embed the IEM RGB→index table and finish `rgba_to_dbz`**

Embed the IEM N0Q color table (256 RGB triples) as `const N0Q_RGB: [[u8;3]; 256]`, then:

```rust
fn rgba_to_dbz(rgba: [u8; 4], lut: &[f64; 256]) -> f64 {
    let key = [rgba[0], rgba[1], rgba[2]];
    match N0Q_RGB.iter().position(|c| *c == key) { Some(i) => lut[i], None => f64::NAN }
}
```

> The PNG is paletted, so each pixel's RGB is exactly one of the 256 ramp entries — `position` is an exact lookup. Cache the reverse map in a `OnceLock<HashMap<[u8;3],usize>>` if a profile shows the scan matters.

- [ ] **Step 5: Run the tests (pass) and commit**

Run: `cargo test -p radar-contour field::n0q::tests`
Expected: PASS.

```bash
git add radar-contour/
git commit -m "radar-contour: N0Q PNG fallback field decode"
```

---

### Task 10: Gaussian smoothing of the dBZ grid

**Files:**
- Create: `radar-contour/src/smooth.rs`

Separable Gaussian blur (two 1-D passes). NaN cells are treated as 0 dBZ for the blur (no-echo background); they fall out of contours via thresholding. (Unchanged.)

- [ ] **Step 1: Write the failing test**

Create `radar-contour/src/smooth.rs`:

```rust
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
```

- [ ] **Step 2: Run it (fails)**

Run: `cargo test -p radar-contour smooth::tests`
Expected: FAIL — `gaussian_blur` undefined.

- [ ] **Step 3: Implement separable blur**

Prepend to `radar-contour/src/smooth.rs`:

```rust
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
```

- [ ] **Step 4: Run the tests (pass)**

Run: `cargo test -p radar-contour smooth::tests`
Expected: PASS (both).

- [ ] **Step 5: Commit**

```bash
git add radar-contour/
git commit -m "radar-contour: separable Gaussian blur on dBZ field"
```

---

### Task 11: Filled isobands per dBZ threshold

**Files:**
- Create: `radar-contour/src/contour.rs`

Use `contour-isobands` to turn the smoothed grid into filled bands. Each band carries a `dbz` lower-bound attribute; polygons are in **grid index space** (lifted to lon/lat in Task 12). (Unchanged.)

- [ ] **Step 1: Write the failing test**

Create `radar-contour/src/contour.rs`:

```rust
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
```

- [ ] **Step 2: Run it (fails)**

Run: `cargo test -p radar-contour contour::tests`
Expected: FAIL — `isobands` undefined.

- [ ] **Step 3: Implement isoband extraction**

Prepend to `radar-contour/src/contour.rs`:

```rust
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
```

> Confirm the `contour-isobands` API (`ContourBuilder::new(w,h)`, `contours(values, thresholds)`, per-band `min_v()`/`geometry()`). Adapt accessor names; the downstream contract is a `Vec<Band>` with ascending `dbz` and grid-space polygons.

- [ ] **Step 4: Run the test (passes)**

Run: `cargo test -p radar-contour contour::tests`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add radar-contour/
git commit -m "radar-contour: filled isobands per dBZ threshold"
```

---

### Task 12: Web Mercator helpers + grid→lonlat lift

**Files:**
- Create: `radar-contour/src/mercator.rs`

(Unchanged from the prior draft.)

- [ ] **Step 1: Write the failing test**

Create `radar-contour/src/mercator.rs`:

```rust
#[cfg(test)]
mod tests {
    use super::*;
    use approx::assert_relative_eq;

    #[test]
    fn lonlat_to_mercator_origin_and_axes() {
        let (x, y) = lonlat_to_merc(0.0, 0.0);
        assert_relative_eq!(x, 0.0, epsilon = 1e-6);
        assert_relative_eq!(y, 0.0, epsilon = 1e-6);
        let (xe, _) = lonlat_to_merc(10.0, 0.0);
        let (_, yn) = lonlat_to_merc(0.0, 10.0);
        assert!(xe > 0.0 && yn > 0.0);
    }

    #[test]
    fn tile_bounds_cover_world_at_z0() {
        let b = tile_bounds_merc(0, 0, 0);
        assert!(b.west < 0.0 && b.east > 0.0 && b.south < 0.0 && b.north > 0.0);
    }
}
```

- [ ] **Step 2: Run it (fails)**

Run: `cargo test -p radar-contour mercator::tests`
Expected: FAIL — undefined functions.

- [ ] **Step 3: Implement mercator + tile math + lift**

Prepend to `radar-contour/src/mercator.rs`:

```rust
use crate::config::BBox;
use crate::field::FieldGrid;
use geo_types::{Coord, LineString, MultiPolygon, Polygon};

const R: f64 = 6_378_137.0;
pub const HALF: f64 = std::f64::consts::PI * R;

pub fn lonlat_to_merc(lon: f64, lat: f64) -> (f64, f64) {
    let x = R * lon.to_radians();
    let y = R * ((std::f64::consts::FRAC_PI_4 + lat.to_radians() / 2.0).tan()).ln();
    (x, y)
}

#[derive(Debug, Clone, Copy)]
pub struct MercBox { pub west: f64, pub south: f64, pub east: f64, pub north: f64 }

pub fn tile_bounds_merc(z: u8, x: u32, y: u32) -> MercBox {
    let n = 2f64.powi(z as i32);
    let size = (2.0 * HALF) / n;
    let west = -HALF + x as f64 * size;
    let east = west + size;
    let north = HALF - y as f64 * size;
    let south = north - size;
    MercBox { west, south, east, north }
}

pub fn tile_range(z: u8, bbox: &BBox) -> (u32, u32, u32, u32) {
    let (wx, ny) = lonlat_to_merc(bbox.west, bbox.north);
    let (ex, sy) = lonlat_to_merc(bbox.east, bbox.south);
    let n = 2f64.powi(z as i32);
    let size = (2.0 * HALF) / n;
    let xmin = (((wx + HALF) / size).floor() as i64).clamp(0, n as i64 - 1) as u32;
    let xmax = (((ex + HALF) / size).floor() as i64).clamp(0, n as i64 - 1) as u32;
    let ymin = (((HALF - ny) / size).floor() as i64).clamp(0, n as i64 - 1) as u32;
    let ymax = (((HALF - sy) / size).floor() as i64).clamp(0, n as i64 - 1) as u32;
    (xmin, ymin, xmax, ymax)
}

/// Map a grid-index coord (col=x, row=y, possibly fractional) to lon/lat.
pub fn grid_to_lonlat(g: &FieldGrid, x: f64, y: f64) -> (f64, f64) {
    (g.lon0 + x * g.dlon, g.lat0 + y * g.dlat)
}

/// Lift a grid-space MultiPolygon to lon/lat.
pub fn lift_multipolygon(g: &FieldGrid, mp: &MultiPolygon<f64>) -> MultiPolygon<f64> {
    let map_ring = |ls: &LineString<f64>| LineString(
        ls.0.iter().map(|c| {
            let (lon, lat) = grid_to_lonlat(g, c.x, c.y);
            Coord { x: lon, y: lat }
        }).collect()
    );
    MultiPolygon(mp.0.iter().map(|poly| {
        Polygon::new(map_ring(poly.exterior()), poly.interiors().iter().map(map_ring).collect())
    }).collect())
}
```

- [ ] **Step 4: Run the tests (pass)**

Run: `cargo test -p radar-contour mercator::tests`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add radar-contour/
git commit -m "radar-contour: web mercator + tile math + grid->lonlat lift"
```

---

### Task 13: Chaikin polygon smoothing

**Files:**
- Create: `radar-contour/src/chaikin.rs`

(Unchanged.)

- [ ] **Step 1: Write the failing test**

Create `radar-contour/src/chaikin.rs`:

```rust
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
```

- [ ] **Step 2: Run it (fails)**

Run: `cargo test -p radar-contour chaikin::tests`
Expected: FAIL — `chaikin_ring` undefined.

- [ ] **Step 3: Implement Chaikin**

Prepend to `radar-contour/src/chaikin.rs`:

```rust
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
```

- [ ] **Step 4: Run the test (passes)**

Run: `cargo test -p radar-contour chaikin::tests`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add radar-contour/
git commit -m "radar-contour: Chaikin polygon smoothing"
```

---

### Task 14: Per-tile clipping + pyramid build (AABB pre-filter + per-zoom simplify)

> **Resolves review #3.** Before any boolean intersection, reject band×tile pairs whose axis-aligned bounding boxes don't overlap (most pairs). Simplify each band per zoom (Douglas–Peucker at a tile-appropriate tolerance) so low zooms don't carry full-res geometry. A perf-budget check on a real CONUS frame is an explicit gate.

**Files:**
- Create: `radar-contour/src/tile.rs`

- [ ] **Step 1: Write the failing test (clip + the AABB pre-filter actually skips work)**

Create `radar-contour/src/tile.rs`:

```rust
#[cfg(test)]
mod tests {
    use super::*;
    use crate::contour::Band;
    use geo_types::{Coord, LineString, MultiPolygon, Polygon};

    fn square_band(dbz: f64, x0: f64, y0: f64, x1: f64, y1: f64) -> Band {
        let ring = LineString(vec![
            Coord{x:x0,y:y0}, Coord{x:x1,y:y0}, Coord{x:x1,y:y1},
            Coord{x:x0,y:y1}, Coord{x:x0,y:y0},
        ]);
        Band { dbz, geom: MultiPolygon(vec![Polygon::new(ring, vec![])]) }
    }

    #[test]
    fn clips_band_to_tile_bounds() {
        let bands = vec![square_band(20.0, -100.0, 30.0, -90.0, 40.0)];
        let bbox = crate::config::BBox { west:-100.0, south:30.0, east:-90.0, north:40.0 };
        let tiles = build_pyramid(&bands, &bbox, 5, 5);
        assert!(!tiles.is_empty());
        assert!(tiles.iter().all(|t| !t.features.is_empty()));
    }

    #[test]
    fn aabb_prefilter_rejects_non_overlapping_pairs() {
        // Band far to the west; a tile-box far to the east must early-reject.
        let band = square_band(20.0, -120.0, 30.0, -119.0, 31.0);
        let bb = band_aabb(&band);
        let east = Aabb { minx:-80.0, miny:30.0, maxx:-79.0, maxy:31.0 };
        assert!(!aabb_overlap(&bb, &east));
    }
}
```

- [ ] **Step 2: Run it (fails)**

Run: `cargo test -p radar-contour tile::tests`
Expected: FAIL — undefined symbols.

- [ ] **Step 3: Implement clipping + pyramid with the pre-filter + simplify**

Prepend to `radar-contour/src/tile.rs`:

```rust
use crate::config::BBox;
use crate::contour::Band;
use crate::mercator::{tile_bounds_merc, tile_range};
use geo::{BooleanOps, Simplify};
use geo_types::{Coord, LineString, MultiPolygon, Polygon};

#[derive(Debug, Clone)]
pub struct TileFeature { pub dbz: f64, pub geom: MultiPolygon<f64> }

#[derive(Debug, Clone)]
pub struct PyramidTile { pub z: u8, pub x: u32, pub y: u32, pub features: Vec<TileFeature> }

#[derive(Debug, Clone, Copy)]
pub struct Aabb { pub minx: f64, pub miny: f64, pub maxx: f64, pub maxy: f64 }

pub fn aabb_overlap(a: &Aabb, b: &Aabb) -> bool {
    a.minx <= b.maxx && a.maxx >= b.minx && a.miny <= b.maxy && a.maxy >= b.miny
}

pub fn band_aabb(b: &Band) -> Aabb {
    let (mut minx, mut miny, mut maxx, mut maxy) = (f64::MAX, f64::MAX, f64::MIN, f64::MIN);
    for poly in &b.geom.0 {
        for c in poly.exterior().0.iter() {
            minx = minx.min(c.x); miny = miny.min(c.y);
            maxx = maxx.max(c.x); maxy = maxy.max(c.y);
        }
    }
    Aabb { minx, miny, maxx, maxy }
}

fn tile_clip_lonlat(z: u8, x: u32, y: u32, buffer_frac: f64) -> (Polygon<f64>, Aabb) {
    let b = tile_bounds_merc(z, x, y);
    let size = b.east - b.west;
    let pad = size * buffer_frac;
    let inv = |mx: f64, my: f64| {
        let lon = (mx / 6_378_137.0).to_degrees();
        let lat = (2.0 * (my / 6_378_137.0).exp().atan() - std::f64::consts::FRAC_PI_2).to_degrees();
        (lon, lat)
    };
    let (w, n) = inv(b.west - pad, b.north + pad);
    let (e, s) = inv(b.east + pad, b.south - pad);
    let poly = Polygon::new(LineString(vec![
        Coord{x:w,y:s}, Coord{x:e,y:s}, Coord{x:e,y:n}, Coord{x:w,y:n}, Coord{x:w,y:s},
    ]), vec![]);
    (poly, Aabb { minx: w, miny: s, maxx: e, maxy: n })
}

/// Per-zoom simplification tolerance (degrees). Low zooms simplify hard; high
/// zooms keep detail. ~ half a tile / EXTENT, clamped.
fn simplify_tol_deg(z: u8) -> f64 {
    let deg_per_tile = 360.0 / 2f64.powi(z as i32);
    (deg_per_tile / 4096.0 * 2.0).clamp(1e-6, 0.05)
}

pub fn build_pyramid(bands: &[Band], bbox: &BBox, min_zoom: u8, max_zoom: u8) -> Vec<PyramidTile> {
    // Precompute each band's AABB once (review #3: cheap reject before ∩).
    let band_boxes: Vec<Aabb> = bands.iter().map(band_aabb).collect();
    let mut tiles = Vec::new();

    for z in min_zoom..=max_zoom {
        let tol = simplify_tol_deg(z);
        // Simplify every band once per zoom, not per tile.
        let simplified: Vec<Band> = bands.iter().map(|b| Band {
            dbz: b.dbz,
            geom: b.geom.simplify(&tol),
        }).collect();

        let (xmin, ymin, xmax, ymax) = tile_range(z, bbox);
        for x in xmin..=xmax {
            for y in ymin..=ymax {
                let (clip_poly, clip_box) = tile_clip_lonlat(z, x, y, 0.05);
                let clip = MultiPolygon(vec![clip_poly]);
                let mut features = Vec::new();
                for (bi, b) in simplified.iter().enumerate() {
                    if !aabb_overlap(&band_boxes[bi], &clip_box) { continue; } // pre-filter
                    let clipped = b.geom.intersection(&clip);
                    if !clipped.0.is_empty() {
                        features.push(TileFeature { dbz: b.dbz, geom: clipped });
                    }
                }
                if !features.is_empty() {
                    tiles.push(PyramidTile { z, x, y, features });
                }
            }
        }
    }
    tiles
}
```

> `geo::BooleanOps::intersection` and `geo::Simplify` are the contracts; adapt names if the installed `geo` differs. The AABB on the *unsimplified* band is a safe over-estimate for the simplified one (simplification never grows the box), so the pre-filter stays correct.

- [ ] **Step 4: Run the tests (pass)**

Run: `cargo test -p radar-contour tile::tests`
Expected: PASS (both).

- [ ] **Step 5: Perf-budget gate (review #3) — do this before wiring the CronJob**

After the end-to-end run exists (Task 18), time `build_pyramid` on one real CONUS frame at the default `grid_deg`:

```bash
RUST_LOG=info target/release/radar-contour   # logs per-stage timing
```

Add a `tracing::info!` span timing around `build_pyramid` in `main.rs`. **Gate:** the full frame (decode → composite → blur → contour → pyramid → MVT → package) must finish comfortably inside the CronJob cadence (Task 21, ~5 min) on `big-bulky-1`. If it overruns: raise `grid_deg`, lower `max_zoom`, or narrow `bbox` — and record the chosen budget in the Task 21 manifest comments. Commit.

```bash
git add radar-contour/
git commit -m "radar-contour: per-tile clip with AABB pre-filter + per-zoom simplify"
```

---

### Task 15: MVT encoding (geozero MvtWriter; correct winding)

> **Resolves review #5.** Use geozero's `MvtWriter`, which emits the MVT command stream and enforces ring winding (exterior CW / holes CCW in tile space) for us — the y-flip from north-up lon/lat to tile space reverses `geo`'s OGC orientation, and renderers drop fills with wrong winding. A hand-rolled encoder is kept only as a fallback, with an explicit winding-repair step.

**Files:**
- Create: `radar-contour/src/mvt.rs`

- [ ] **Step 1: Write the failing test (encode + round-trip decode keeps the fill)**

Create `radar-contour/src/mvt.rs`:

```rust
#[cfg(test)]
mod tests {
    use super::*;
    use crate::tile::{PyramidTile, TileFeature};
    use geo_types::{Coord, LineString, MultiPolygon, Polygon};

    fn tile_with_centered_square() -> PyramidTile {
        let b = crate::mercator::tile_bounds_merc(5, 8, 12);
        let midx = (b.west + b.east) / 2.0;
        let midy = (b.south + b.north) / 2.0;
        let lon = (midx / 6_378_137.0).to_degrees();
        let lat = (2.0*(midy/6_378_137.0).exp().atan() - std::f64::consts::FRAC_PI_2).to_degrees();
        let d = 0.2;
        let ring = LineString(vec![
            Coord{x:lon-d,y:lat-d}, Coord{x:lon+d,y:lat-d},
            Coord{x:lon+d,y:lat+d}, Coord{x:lon-d,y:lat+d}, Coord{x:lon-d,y:lat-d},
        ]);
        PyramidTile { z:5, x:8, y:12, features: vec![
            TileFeature { dbz: 35.0, geom: MultiPolygon(vec![Polygon::new(ring, vec![])]) }
        ]}
    }

    #[test]
    fn encodes_nonempty_mvt() {
        let bytes = encode_tile(&tile_with_centered_square());
        assert!(!bytes.is_empty());
    }

    #[test]
    fn roundtrip_has_radar_layer_with_dbz_and_a_polygon() {
        let bytes = encode_tile(&tile_with_centered_square());
        let (layer, has_dbz, has_polygon) = decode_summary(&bytes);
        assert_eq!(layer, "radar");
        assert!(has_dbz);
        assert!(has_polygon, "polygon survived (winding correct -> fill kept)");
    }
}
```

- [ ] **Step 2: Run it (fails)**

Run: `cargo test -p radar-contour mvt::tests`
Expected: FAIL — `encode_tile`/`decode_summary` undefined.

- [ ] **Step 3: Implement via geozero MvtWriter**

Prepend to `radar-contour/src/mvt.rs`:

```rust
use crate::mercator::{tile_bounds_merc, lonlat_to_merc};
use crate::tile::PyramidTile;
use geo_types::{Coord, LineString, MultiPolygon, Polygon};

const EXTENT: u32 = 4096;

/// Project a lon/lat polygon into this tile's 0..EXTENT space (y points down
/// from the north edge). Winding is repaired by geozero on encode.
fn project_polygon(z: u8, x: u32, y: u32, poly: &Polygon<f64>) -> Polygon<f64> {
    let b = tile_bounds_merc(z, x, y);
    let to_local = |c: &Coord<f64>| {
        let (mx, my) = lonlat_to_merc(c.x, c.y);
        Coord {
            x: ((mx - b.west) / (b.east - b.west) * EXTENT as f64),
            y: ((b.north - my) / (b.north - b.south) * EXTENT as f64),
        }
    };
    let ring = |ls: &LineString<f64>| LineString(ls.0.iter().map(to_local).collect());
    Polygon::new(ring(poly.exterior()), poly.interiors().iter().map(ring).collect())
}

/// Encode a PyramidTile to MVT via geozero's MvtWriter (layer `radar`,
/// integer `dbz` per feature). MvtWriter handles command encoding AND ring
/// winding (review #5).
pub fn encode_tile(t: &PyramidTile) -> Vec<u8> {
    use geozero::mvt::{MvtWriter, TileValue};
    use geozero::{ColumnValue, FeatureProcessor, GeozeroGeometry, PropertyProcessor};

    let mut writer = MvtWriter::new("radar", EXTENT);
    writer.dataset_begin(None).unwrap();
    for (fid, f) in t.features.iter().enumerate() {
        // Project all rings to tile space first.
        let projected = MultiPolygon(
            f.geom.0.iter().map(|p| project_polygon(t.z, t.x, t.y, p)).collect());
        writer.feature_begin(fid as u64).unwrap();
        writer.properties_begin().unwrap();
        writer.property(0, "dbz", &ColumnValue::Long(f.dbz as i64)).unwrap();
        writer.properties_end().unwrap();
        writer.geometry_begin().unwrap();
        projected.process_geom(&mut writer).unwrap(); // emits commands + winding
        writer.geometry_end().unwrap();
        writer.feature_end(fid as u64).unwrap();
        let _ = TileValue::Int(0); // keep import honest
    }
    writer.dataset_end().unwrap();
    writer.into_bytes()
}

/// Decode for the round-trip test: returns (first layer name, has dbz key,
/// has at least one polygon feature).
pub fn decode_summary(bytes: &[u8]) -> (String, bool, bool) {
    use geozero::mvt::Tile;
    use geozero::mvt::Message;
    let tile = Tile::decode(bytes).expect("decode mvt");
    let layer = tile.layers.first().expect("a layer");
    let has_dbz = layer.keys.iter().any(|k| k == "dbz");
    let has_polygon = layer.features.iter().any(|f| f.r#type == Some(3)); // GeomType::Polygon
    (layer.name.clone(), has_dbz, has_polygon)
}
```

> geozero's exact `MvtWriter` surface varies by version (`MvtWriter::new`, the `FeatureProcessor`/`GeomProcessor`/`PropertyProcessor` trait methods, `into_bytes`). Confirm against the installed crate; the contract is: a valid MVT tile, layer `radar`, integer `dbz` per feature, 4096 extent, **with correct ring winding so renderers keep the fill**. If your geozero version lacks a ready `MvtWriter`, hand-roll the command encoder but add an explicit winding step: after projecting, force exterior rings clockwise and holes counter-clockwise in tile space (signed-area test, reverse if needed) before emitting commands. The `roundtrip_has...polygon` test is what proves winding is right.

- [ ] **Step 4: Run the tests (pass)**

Run: `cargo test -p radar-contour mvt::tests`
Expected: PASS (both).

- [ ] **Step 5: Commit**

```bash
git add radar-contour/
git commit -m "radar-contour: MVT encoding via geozero MvtWriter (correct winding)"
```

---

### Task 16: Package the pyramid into one PMTiles archive

**Files:**
- Create: `radar-contour/src/package.rs`

Stage every tile into a temporary MBTiles (SQLite), then convert to PMTiles with the `pmtiles` CLI. One `.pmtiles` per frame. (Unchanged.)

- [ ] **Step 1: Write the failing test**

Create `radar-contour/src/package.rs`:

```rust
#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn writes_mbtiles_with_tiles_and_metadata() {
        let raw = vec![(3u8, 1u32, 2u32, vec![1,2,3u8])];
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
```

- [ ] **Step 2: Run it (fails)**

Run: `cargo test -p radar-contour package::tests`
Expected: FAIL — `write_mbtiles` undefined.

- [ ] **Step 3: Implement MBTiles writer + PMTiles conversion**

Prepend to `radar-contour/src/package.rs`:

```rust
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
```

- [ ] **Step 4: Run the test (passes)**

Run: `cargo test -p radar-contour package::tests`
Expected: PASS. (`mbtiles_to_pmtiles` is covered by the Task 18 smoke test where the `pmtiles` binary exists.)

- [ ] **Step 5: Commit**

```bash
git add radar-contour/
git commit -m "radar-contour: MBTiles staging + pmtiles conversion"
```

---

### Task 17: R2 publish + atomic latest.json flip + change detection (robust read_latest)

> **Resolves review #4.** `read_latest` distinguishes a genuine missing pointer (`NoSuchKey` → `Ok(None)`) from a transient R2/list/auth error (→ propagate). A blip no longer reads as "no current frame," so change-detection isn't defeated and we don't accumulate orphan archives.

**Files:**
- Create: `radar-contour/src/publish.rs`

- [ ] **Step 1: Write the failing test**

Create `radar-contour/src/publish.rs`:

```rust
#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn latest_json_roundtrips() {
        let l = Latest { ts: "20260612T010203Z".into(),
                         key: "radar/20260612T010203Z.pmtiles".into(),
                         minzoom: 3, maxzoom: 10 };
        let s = serde_json::to_string(&l).unwrap();
        let back: Latest = serde_json::from_str(&s).unwrap();
        assert_eq!(back.ts, l.ts);
        assert_eq!(back.key, l.key);
    }

    #[test]
    fn skips_when_frame_unchanged() {
        assert!(should_skip(Some("frameA"), "frameA"));
        assert!(!should_skip(Some("frameA"), "frameB"));
        assert!(!should_skip(None, "frameA"));
    }
}
```

- [ ] **Step 2: Run it (fails)**

Run: `cargo test -p radar-contour publish::tests`
Expected: FAIL — `Latest`/`should_skip` undefined.

- [ ] **Step 3: Implement publish logic with robust read_latest**

Prepend to `radar-contour/src/publish.rs`:

```rust
use anyhow::{anyhow, Result};
use aws_sdk_s3::primitives::ByteStream;
use aws_sdk_s3::Client;
use serde::{Deserialize, Serialize};
use std::path::Path;

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Latest {
    pub ts: String,
    pub key: String,
    pub minzoom: u8,
    pub maxzoom: u8,
}

pub fn should_skip(current_latest_ts: Option<&str>, new_frame_ts: &str) -> bool {
    current_latest_ts == Some(new_frame_ts)
}

/// Atomic publish: PUT the archive, then overwrite latest.json. A crash
/// between the two leaves a harmless orphan archive, never a half-served frame.
pub async fn publish_frame(
    client: &Client, bucket: &str, prefix: &str, ts: &str,
    pmtiles_path: &Path, minzoom: u8, maxzoom: u8,
) -> Result<()> {
    let key = format!("{prefix}/{ts}.pmtiles");
    let body = ByteStream::from_path(pmtiles_path).await?;
    client.put_object().bucket(bucket).key(&key)
        .content_type("application/octet-stream").body(body).send().await?;

    let latest = Latest { ts: ts.into(), key: key.clone(), minzoom, maxzoom };
    let latest_key = format!("{prefix}/latest.json");
    client.put_object().bucket(bucket).key(&latest_key)
        .content_type("application/json").cache_control("no-cache")
        .body(ByteStream::from(serde_json::to_vec(&latest)?)).send().await?;
    Ok(())
}

/// Read current latest.json. Missing pointer -> Ok(None). ANY OTHER error
/// (network/auth/parse) propagates, so a transient blip fails the run loudly
/// instead of silently republishing every cycle (review #4).
pub async fn read_latest(client: &Client, bucket: &str, prefix: &str) -> Result<Option<Latest>> {
    let key = format!("{prefix}/latest.json");
    match client.get_object().bucket(bucket).key(&key).send().await {
        Ok(resp) => {
            let bytes = resp.body.collect().await?.into_bytes();
            let latest: Latest = serde_json::from_slice(&bytes)
                .map_err(|e| anyhow!("latest.json parse: {e}"))?;
            Ok(Some(latest))
        }
        Err(sdk_err) => {
            // Distinguish a genuinely-absent key from a real error.
            let svc = sdk_err.into_service_error();
            if svc.is_no_such_key() {
                Ok(None)
            } else {
                Err(anyhow!("read latest.json: {svc}"))
            }
        }
    }
}
```

> `aws-sdk-s3`'s `GetObjectError` exposes `is_no_such_key()`; some R2 responses surface a missing object as a 404 without the typed `NoSuchKey` variant. Confirm against R2: if R2 returns an untyped 404, match on the HTTP status via `ProvideErrorMetadata`/`.code()` (`"NoSuchKey"`) instead. The contract: missing → `None`; everything else → `Err`.

- [ ] **Step 4: Run the tests (pass)**

Run: `cargo test -p radar-contour publish::tests`
Expected: PASS (both).

- [ ] **Step 5: Commit**

```bash
git add radar-contour/
git commit -m "radar-contour: R2 publish, atomic latest.json, robust read_latest"
```

---

### Task 18: Wire the end-to-end run in `main.rs`

**Files:**
- Modify: `radar-contour/src/main.rs`

- [ ] **Step 1: Implement the orchestration (async source, awaited)**

Replace `main.rs` body:

```rust
use anyhow::Result;
use radar_contour::{config::Config, field::{ReflectivityField, level2::Level2Source},
    smooth, contour, chaikin, mercator, tile, mvt, package, publish};
use clap::Parser;

#[tokio::main]
async fn main() -> Result<()> {
    tracing_subscriber::fmt().with_env_filter(
        std::env::var("RUST_LOG").unwrap_or_else(|_| "info".into())).init();
    let cfg = Config::parse();

    // v1 field source: Level II super-res, composited over the bbox.
    let cadence_s = 300; // matches the CronJob window (Task 21)
    let source = Level2Source::new(cfg.bbox, cfg.grid_deg, cadence_s);
    let frame = source.latest_frame_id().await?;

    let r2 = build_r2_client(&cfg).await?;
    let bucket = cfg.r2_bucket.clone().expect("RADAR_R2_BUCKET");
    let current = publish::read_latest(&r2, &bucket, &cfg.r2_prefix).await?;
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
    let mbtiles = tmp.join("frame.mbtiles");
    let pmtiles = tmp.join("frame.pmtiles");
    package::write_mbtiles(&mbtiles, &raw, cfg.min_zoom, cfg.max_zoom)?;
    package::mbtiles_to_pmtiles(&mbtiles, &pmtiles)?;

    let ts = sanitize_ts(&frame);
    publish::publish_frame(&r2, &bucket, &cfg.r2_prefix, &ts, &pmtiles,
        cfg.min_zoom, cfg.max_zoom).await?;
    tracing::info!(%ts, "published frame");
    Ok(())
}
```

Add the helpers `build_r2_client` (S3 client with `endpoint_url = cfg.r2_endpoint`, `force_path_style`, region `auto`, creds from env `RADAR_R2_ACCESS_KEY_ID`/`RADAR_R2_SECRET_ACCESS_KEY`), `tempdir_path`, and `sanitize_ts` (frame id → `YYYYMMDDTHHMMSSZ`).

- [ ] **Step 2: Build**

Run: `cargo build -p radar-contour --release`
Expected: compiles.

- [ ] **Step 3: End-to-end smoke test (manual; creds + `pmtiles` + AWS access)**

```bash
export RADAR_R2_BUCKET=... RADAR_R2_ENDPOINT=https://<acct>.r2.cloudflarestorage.com
export RADAR_R2_ACCESS_KEY_ID=... RADAR_R2_SECRET_ACCESS_KEY=...
# Narrow the bbox first to keep the smoke test fast (one region, a few sites):
target/release/radar-contour --bbox-... # or set a regional default while testing
aws s3 ls s3://$RADAR_R2_BUCKET/radar/ --endpoint-url $RADAR_R2_ENDPOINT
pmtiles show radar/<ts>.pmtiles
```
Expected: one `<ts>.pmtiles` + `latest.json`; `pmtiles show` reports a `radar` vector layer, z3–z10; the per-stage timing log confirms the perf budget (Task 14 Step 5).

- [ ] **Step 4: Commit**

```bash
git add radar-contour/
git commit -m "radar-contour: end-to-end Level II frame build and publish"
```

---

## Phase B — Origin Worker + deploy (graywolf-maps repo)

> Not in this workspace. Check out graywolf-maps, read `~/dev/graywolf-maps/.context/graywolf-client-integration.md`, and mirror the existing basemap PMTiles range-serve handler.

### Task 19: Worker radar tile route (range-serve from PMTiles in R2)

**Files (graywolf-maps):**
- Create: `src/radar.ts`
- Modify: the Worker router to mount the radar routes

- [ ] **Step 1:** Write the failing test (Miniflare/Vitest): `GET /radar/<ts>/5/8/12.mvt` against a fixture PMTiles in a mock R2 bucket → 200 + `application/x-protobuf` + non-empty body; empty tile → 204/404.
- [ ] **Step 2:** Run it — fails (route missing).
- [ ] **Step 3:** Implement using the `pmtiles` JS library's R2 source: parse `:ts/:z/:x/:y`, open `radar/<ts>.pmtiles` via range reads, return tile bytes with `Content-Type: application/x-protobuf`, `Content-Encoding: gzip` (tiles were gzip'd in Task 16), long immutable `Cache-Control` (each frame URL is unique). Add `GET /radar/latest.json` streaming the R2 object with `Cache-Control: no-cache`. Reuse the basemap PMTiles handler — same range-serve pattern, different prefix.
- [ ] **Step 4:** Run tests — pass.
- [ ] **Step 5:** Commit + deploy to a preview; smoke-test against a real published frame.

### Task 20: Auth + CORS parity with the basemap route

- [ ] **Step 1:** Confirm whether radar tiles are auth-gated like `maps.nw5w.com` tiles (bearer `?t=` token) or public. **Decision needed from owner.** Default: mirror basemap auth so the client's existing `transformRequest` token path applies unchanged.
- [ ] **Step 2:** Ensure CORS-simple responses (no preflight), matching the basemap. Commit.

### Task 21: k8s CronJob on big-bulky-1

**Files (graywolf-maps):**
- Create: `packaging/k8s/radar-contour-cronjob.yaml`

- [ ] **Step 1:** Build a container image: minimal base + the `radar-contour` release binary + the `pmtiles` CLI. (Build the binary from this repo; copy it into the image, or publish to a registry the CronJob pulls. Level II adds no extra system deps — the `nexrad-*` stack is pure Rust.)
- [ ] **Step 2:** Write the CronJob: `schedule: "*/5 * * * *"` (5-min cadence — a Level II volume scan completes every ~4–6 min per site), `concurrencyPolicy: Forbid`, `successfulJobsHistoryLimit: 1`, `failedJobsHistoryLimit: 3`, `restartPolicy: Never`, node affinity pinning to `big-bulky-1`, **resource requests sized from the Task 14 perf measurement** (the multi-site decode + 250 m composite + contour is the heavy pass — this is what `big-bulky-1` is for), and R2 creds from a `Secret` (`RADAR_R2_*`). The change-detection skip (Task 17) keeps the cadence cheap when no site produced a new volume. Set the generator's `cadence_s` (Task 18) to match the 300 s window.
- [ ] **Step 3:** `kubectl apply`, watch the first runs (`kubectl logs`), confirm objects land in R2 and the frame builds inside the cadence. Commit.

---

## Phase C — Client (this repo)

> The client is **identical regardless of field source** — it consumes `radar/<ts>.pmtiles` tiles with a `dbz` attribute. Tasks 22–26 are unchanged by the Level II pivot.

### Task 22: dBZ palette + source/layer specs

**Files:**
- Create: `web/src/lib/map/sources/radar-source.js`

- [ ] **Step 1: Write the failing test**

Create `web/src/lib/map/sources/radar-source.test.js`:

```js
import { describe, it, expect } from 'vitest';
import { DBZ_THRESHOLDS, DBZ_COLORS, fillColorExpression } from './radar-source.js';

describe('radar-source', () => {
  it('has NWS breakpoints 5..75 by 5', () => {
    expect(DBZ_THRESHOLDS).toEqual([5,10,15,20,25,30,35,40,45,50,55,60,65,70,75]);
  });
  it('maps every threshold to a hex color', () => {
    for (const t of DBZ_THRESHOLDS) expect(DBZ_COLORS[t]).toMatch(/^#[0-9a-f]{6}$/i);
  });
  it('builds a maplibre step expression keyed on dbz', () => {
    const expr = fillColorExpression();
    expect(expr[0]).toBe('step');
    expect(expr[1]).toEqual(['get', 'dbz']);
  });
});
```

- [ ] **Step 2: Run it (fails)**

Run: `cd web && npx vitest run src/lib/map/sources/radar-source.test.js`
Expected: FAIL — module not found.

- [ ] **Step 3: Implement the palette + specs**

Create `web/src/lib/map/sources/radar-source.js`:

```js
// Canonical NWS reflectivity ramp. Each key is the lower-bound dBZ of a
// filled isoband (matches the `dbz` attribute the generator writes per
// polygon). Coloring is client-side so the same tiles recolor without
// regenerating.
export const DBZ_THRESHOLDS = [5,10,15,20,25,30,35,40,45,50,55,60,65,70,75];

export const DBZ_COLORS = {
  5:'#04e9e7', 10:'#019ff4', 15:'#0300f4', 20:'#02fd02', 25:'#01c501',
  30:'#008e00', 35:'#fdf802', 40:'#e5bc00', 45:'#fd9500', 50:'#fd0000',
  55:'#d40000', 60:'#bc0000', 65:'#f800fd', 70:'#9854c6', 75:'#fdfdfd',
};

export function fillColorExpression() {
  const expr = ['step', ['get', 'dbz'], 'rgba(0,0,0,0)'];
  for (const t of DBZ_THRESHOLDS) expr.push(t, DBZ_COLORS[t]);
  return expr;
}

export const RADAR_SOURCE_ID = 'radar';
export const RADAR_LAYER_ID = 'radar-fill';
export const RADAR_LATEST_URL = 'https://maps.nw5w.com/radar/latest.json';

export function radarTileUrl(ts) {
  return `https://maps.nw5w.com/radar/${ts}/{z}/{x}/{y}.mvt`;
}
```

- [ ] **Step 4: Run the tests (pass)**

Run: `cd web && npx vitest run src/lib/map/sources/radar-source.test.js`
Expected: PASS (all three).

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/map/sources/radar-source.js web/src/lib/map/sources/radar-source.test.js
git commit -m "map: NWS dBZ palette and radar source specs"
```

---

### Task 23: Radar layer module

**Files:**
- Create: `web/src/lib/map/layers/radar.js`

Mirror the existing layer-module contract (`mountWeatherLayer` returns `{ setVisible, ... }`). The radar layer manages a vector source + fill layer, polls `latest.json`, and advances the frame by swapping the source tile URL. Exposes `setVisible`, `setOpacity`, `refresh`, `startPolling`, `stopPolling`, `destroy`.

- [ ] **Step 1: Write the failing test**

Create `web/src/lib/map/layers/radar.test.js`:

```js
import { describe, it, expect, vi } from 'vitest';
import { mountRadarLayer } from './radar.js';

function fakeMap() {
  const sources = {}, layers = {}, paint = {}, layout = {};
  return {
    addSource: vi.fn((id, s) => { sources[id] = s; }),
    getSource: vi.fn((id) => sources[id] ? { setTiles: vi.fn((t)=>{sources[id].tiles=t;}) } : undefined),
    addLayer: vi.fn((l) => { layers[l.id] = l; }),
    getLayer: vi.fn((id) => layers[id]),
    setPaintProperty: vi.fn((id,k,v)=>{ paint[`${id}.${k}`]=v; }),
    setLayoutProperty: vi.fn((id,k,v)=>{ layout[`${id}.${k}`]=v; }),
    _sources: sources, _layers: layers, _paint: paint, _layout: layout,
  };
}

describe('mountRadarLayer', () => {
  it('adds a vector source and a fill layer', async () => {
    const map = fakeMap();
    const layer = mountRadarLayer(map, { fetchLatest: async () => ({ ts: 'T1' }) });
    await layer.refresh();
    expect(map.addSource).toHaveBeenCalled();
    expect(map._sources.radar.type).toBe('vector');
    expect(map._layers['radar-fill'].type).toBe('fill');
  });

  it('setOpacity drives fill-opacity', () => {
    const map = fakeMap();
    const layer = mountRadarLayer(map, { fetchLatest: async () => ({ ts: 'T1' }) });
    layer.setOpacity(0.5);
    expect(map._paint['radar-fill.fill-opacity']).toBe(0.5);
  });

  it('setVisible toggles layout visibility', () => {
    const map = fakeMap();
    const layer = mountRadarLayer(map, { fetchLatest: async () => ({ ts: 'T1' }) });
    layer.setVisible(false);
    expect(map._layout['radar-fill.visibility']).toBe('none');
  });
});
```

- [ ] **Step 2: Run it (fails)**

Run: `cd web && npx vitest run src/lib/map/layers/radar.test.js`
Expected: FAIL — module not found.

- [ ] **Step 3: Implement the layer**

Create `web/src/lib/map/layers/radar.js`:

```js
// Radar layer: a single vector source of smoothed dBZ isobands, painted as
// recolorable fill. Polls latest.json and swaps the source tile template when
// a new frame appears (each frame's URL is unique, so MapLibre's tile cache
// never serves a stale frame). Mirrors the stations/trails/weather layer
// contract: returns { refresh, destroy, setVisible, setOpacity, ... }.
import {
  RADAR_SOURCE_ID, RADAR_LAYER_ID, RADAR_LATEST_URL,
  fillColorExpression, radarTileUrl,
} from '../sources/radar-source.js';

const DEFAULT_OPACITY = 0.7;

export function mountRadarLayer(map, opts = {}) {
  const fetchLatest = opts.fetchLatest ?? (async () => {
    const res = await fetch(RADAR_LATEST_URL, { cache: 'no-cache' });
    if (!res.ok) throw new Error(`radar latest: ${res.status}`);
    return res.json();
  });
  const minzoom = opts.minzoom ?? 3;
  const maxzoom = opts.maxzoom ?? 10;

  let currentTs = null, opacity = DEFAULT_OPACITY, visible = true;

  function ensureLayer(ts) {
    if (!map.getSource(RADAR_SOURCE_ID)) {
      map.addSource(RADAR_SOURCE_ID, { type: 'vector', tiles: [radarTileUrl(ts)], minzoom, maxzoom });
    }
    if (!map.getLayer(RADAR_LAYER_ID)) {
      map.addLayer({
        id: RADAR_LAYER_ID, type: 'fill', source: RADAR_SOURCE_ID, 'source-layer': 'radar',
        paint: { 'fill-color': fillColorExpression(), 'fill-opacity': opacity, 'fill-antialias': true },
        layout: { visibility: visible ? 'visible' : 'none' },
      });
    }
  }

  async function refresh() {
    let latest;
    try { latest = await fetchLatest(); } catch { return; }
    if (!latest || !latest.ts) return;
    ensureLayer(latest.ts);
    if (latest.ts !== currentTs) {
      currentTs = latest.ts;
      const src = map.getSource(RADAR_SOURCE_ID);
      if (src && src.setTiles) src.setTiles([radarTileUrl(latest.ts)]);
    }
  }

  function setOpacity(next) {
    opacity = next;
    if (map.getLayer(RADAR_LAYER_ID)) map.setPaintProperty(RADAR_LAYER_ID, 'fill-opacity', opacity);
  }
  function setVisible(next) {
    visible = !!next;
    if (map.getLayer(RADAR_LAYER_ID)) map.setLayoutProperty(RADAR_LAYER_ID, 'visibility', visible ? 'visible' : 'none');
  }

  let timer = null;
  function startPolling(intervalMs = 300000) { stopPolling(); timer = setInterval(() => { refresh(); }, intervalMs); }
  function stopPolling() { if (timer) { clearInterval(timer); timer = null; } }
  function destroy() {
    stopPolling();
    if (map.getLayer(RADAR_LAYER_ID)) map.removeLayer(RADAR_LAYER_ID);
    if (map.getSource(RADAR_SOURCE_ID)) map.removeSource(RADAR_SOURCE_ID);
  }

  return { refresh, destroy, setVisible, setOpacity, startPolling, stopPolling };
}
```

- [ ] **Step 4: Run the tests (pass)**

Run: `cd web && npx vitest run src/lib/map/layers/radar.test.js`
Expected: PASS (all three).

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/map/layers/radar.js web/src/lib/map/layers/radar.test.js
git commit -m "map: radar vector fill layer module with frame polling"
```

---

### Task 24: Mount the radar layer in LiveMapV2

**Files:**
- Modify: `web/src/routes/LiveMapV2.svelte`

- [ ] **Step 1:** Import + mount next to the other layers:

```js
import { mountRadarLayer } from '$lib/map/layers/radar.js';
```
(match the project's existing alias style). In the map-ready block, next to `weatherLayer = mountWeatherLayer(...)`:

```js
radarLayer = mountRadarLayer(map, { minzoom: 3, maxzoom: 10 });
radarLayer.refresh();
radarLayer.startPolling(300000); // 5-min, matches the CronJob cadence
```
Declare `let radarLayer = null;` with the other handles.

- [ ] **Step 2:** Add toggle + opacity to the `layerToggles` state object:

```js
let layerToggles = $state({
  stations: true, trails: true, weather: true, myPosition: true,
  directRxOnly: false, radar: true, radarOpacity: 0.7,
});
```

- [ ] **Step 3:** Bind effects next to the other toggle `$effect`s:

```js
$effect(() => { radarLayer?.setVisible(layerToggles.radar); });
$effect(() => { radarLayer?.setOpacity(layerToggles.radarOpacity); });
```

- [ ] **Step 4:** Add UI controls in `panelBody` after the Weather toggle:

```svelte
<label class="toggle-row">
  <input type="checkbox" checked={layerToggles.radar}
    onchange={(e) => (layerToggles.radar = e.currentTarget.checked)} />
  <span>Radar</span>
</label>
<label class="toggle-row">
  <span>Radar opacity</span>
  <input type="range" min="0" max="1" step="0.05"
    value={layerToggles.radarOpacity}
    oninput={(e) => (layerToggles.radarOpacity = Number(e.currentTarget.value))} />
</label>
```

- [ ] **Step 5:** Build/lint/manual check, commit:

Run: `cd web && npm run build` (or the repo's check target). Load `/map`, toggle Radar, drag the opacity slider, confirm bands paint over the basemap and recolor/fade correctly.

```bash
git add web/src/routes/LiveMapV2.svelte
git commit -m "map: wire radar layer toggle and opacity slider into LiveMapV2"
```

---

### Task 25: Frame-advance integration check

- [ ] **Step 1:** With the generator publishing live frames, leave `/map` open past one cadence (>5 min) and confirm the layer advances to the new frame without a flicker or stale tiles (the unique per-`ts` URL guarantees clean cache behavior). If MapLibre keeps the prior source around, confirm `setTiles` is enough; if not, recreate the source on frame change.
- [ ] **Step 2:** Confirm fail-soft behavior: if `latest.json` fetch fails, `refresh()` silently no-ops and the rest of the map is unaffected (mirrors the basemap's posture). Commit any fixes.

---

## Phase D — Docs

### Task 26: Wiki + handbook updates

**Files:**
- Modify: `docs/wiki/system-topology.md`, `docs/wiki/code-map.md`

- [ ] **Step 1:** Document the new external service row (radar tiles on `maps.nw5w.com/radar/*`), the R2 object layout (`radar/<ts>.pmtiles` + `radar/latest.json`), the **Level II v1 field source** (per-site super-res composite) with MRMS/N0Q as fallbacks behind the trait, the generator placement decision, and the resolution/perf levers (`grid_deg`, `bbox`). Add `radar-contour/` and the client radar files to the code map. The graywolf CLAUDE.md makes this **required**.
- [ ] **Step 2:** Commit.

---

## Self-Review

**Spec + owner-direction coverage:**
- v1 field = **Level II super-res, per-site** (owner direction) → Tasks 4–7 (catalog, decode, geolocation, multi-site composite). MRMS/N0Q kept as fallbacks behind the trait → Tasks 8–9. ✅
- Detect new frame / skip stale → Task 17 (`should_skip`) + Task 18 + Task 21 (5-min CronJob). ✅
- Smooth (Gaussian) → Task 10. Contour (filled isobands, `dbz` lower-bound, NWS 5..75) → Task 11 + Task 2 thresholds. Smooth polygons (Chaikin) → Task 13. ✅
- Tile (reproject Web Mercator, clip + buffer, MVT, z3–z10) → Tasks 12, 14, 15. ✅
- Package + publish atomically (one PMTiles/frame, flip `latest.json` last) → Tasks 16–17. PMTiles-per-frame cost decision (one PUT/frame) preserved. ✅
- Worker range-serves tiles → Task 19. Client vector source + `fill` `step` on `dbz` + opacity slider → Tasks 22–24. ✅

**Review issues resolved (High + Medium):**
- High #1 (sync trait vs async S3) → Task 3 async trait; awaited in Task 18; enables concurrent multi-site fetch (Task 7). ✅
- High #2 (MRMS 0–360 lon) → Task 8 normalization + CONUS-range test; Level II analogue (gate geolocation) locked by Task 6's due-east test. ✅
- Medium #3 (boolean ∩ blowup) → Task 14 AABB pre-filter + per-zoom Douglas–Peucker + explicit perf-budget gate. ✅
- Medium #4 (`read_latest` swallows errors) → Task 17 `NoSuchKey` → `None`, else propagate. ✅
- Medium #5 (MVT winding under y-flip) → Task 15 geozero `MvtWriter` (+ winding-repair fallback) + round-trip polygon-survival test. ✅
- Low/nits (half-cell registration, blur re-mask comment, `lats[cols]` assumption) noted inline; not gated per request.

**Type consistency:** `FieldGrid`/`GridSpec` (Task 3) feed the Level II compositor (Tasks 6–7) and the pipeline (Tasks 10–12); `Band { dbz, geom }` (Task 11) flows through Tasks 13–14; `PyramidTile`/`TileFeature` (Task 14) feed Task 15; `RawTile` (Task 16) feeds Tasks 17–18; client `RADAR_SOURCE_ID`/`fillColorExpression`/`radarTileUrl` (Task 22) are the exact names used in Task 23; `Latest { ts, key, minzoom, maxzoom }` (Task 17) is what the client reads and the Worker serves. Consistent.

---

## Open Decisions (carry to the owner; do not block scaffolding)

1. **CONUS vs regional extent at 250 m.** Level II at full `grid_deg=0.0025` over CONUS is the heaviest configuration. The perf-budget gate (Task 14 Step 5) decides whether v1 ships CONUS or starts regional; the levers (`grid_deg`, `bbox`, `max_zoom`) are all config, no re-architecture. Recommend validating CONUS first, falling back to regional if the cadence can't be met on `big-bulky-1`.
2. **Radar tile auth:** gate behind the existing bearer-token scheme (Task 20 default) vs public.
3. **Tuning constants:** `gaussian_sigma` (1.0), `chaikin_iterations` (2), `grid_deg` (0.0025) — tune visually against the target screenshots once frames render.
4. **Lowest-tilt only vs multi-tilt hybrid scan.** v1 composites the 0.5° super-res reflectivity cut. A future enhancement could blend higher tilts near the radar to reduce cone-of-silence gaps; out of scope for v1.
5. **Cadence:** 5-min CronJob (matches a Level II volume scan) with change-detection skip. Confirm against desired freshness.
6. **R2 retention / loop window:** v1 keeps `latest` + orphans; add a lifecycle rule or prune step when animation lands. Out of scope here.
