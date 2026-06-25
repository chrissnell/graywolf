//! End-to-end: run `graywolf-modem --decode <track>` on a known-good track and
//! assert it reports decoded frames as JSON. Skips when the fixtures are absent
//! (same convention as ipc_flac_e2e.rs).

use std::path::PathBuf;
use std::process::Command;

fn binary() -> PathBuf {
    // The repo is a cargo workspace (root Cargo.toml lists graywolf-modem
    // as a member), so cargo writes build artifacts to <workspace>/target
    // — the parent of this crate's manifest directory, not inside it.
    let manifest_dir = PathBuf::from(env!("CARGO_MANIFEST_DIR"));
    let workspace_root = manifest_dir.parent().expect("manifest has a parent");
    let mut p = workspace_root.to_path_buf();
    p.push("target");
    p.push(if cfg!(debug_assertions) { "debug" } else { "release" });
    p.push("graywolf-modem");
    p
}

fn test_track() -> Option<PathBuf> {
    let manifest = PathBuf::from(env!("CARGO_MANIFEST_DIR"));
    // Prefer the small committed AFSK-1200 fixture so this test actually runs
    // in CI / a clean checkout; fall back to the large (optional, gitignored)
    // WA8LMF track if someone has it locally.
    let candidates = [
        manifest.join("testdata/wav/afsk_1200.wav"),
        manifest.join("aprs-test-tracks/03_100-Mic-E-Bursts-Flat.flac"),
    ];
    candidates.into_iter().find(|p| p.exists())
}

#[test]
fn decode_reports_frames_for_known_track() {
    let track = match test_track() {
        Some(p) => p,
        None => {
            eprintln!("skipping: aprs-test-tracks not present");
            return;
        }
    };
    let bin = binary();
    if !bin.exists() {
        eprintln!("skipping: graywolf-modem binary not built");
        return;
    }
    let out = Command::new(&bin)
        .arg("--decode")
        .arg(&track)
        .output()
        .expect("run --decode");
    assert!(
        out.status.success(),
        "decode exited non-zero: {}",
        String::from_utf8_lossy(&out.stderr)
    );
    let stdout = String::from_utf8_lossy(&out.stdout);
    let v: serde_json::Value = serde_json::from_str(&stdout).expect("valid JSON");
    let frames = v["rx_frames"].as_u64().expect("rx_frames");
    assert!(frames > 0, "expected >0 decoded frames, got {frames}");
}
