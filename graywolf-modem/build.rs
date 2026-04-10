//! Generates Rust bindings from `../proto/graywolf.proto` via prost-build.
//! The output is included by `src/ipc/proto.rs`.

use std::path::{Path, PathBuf};
use std::process::Command;

fn main() {
    // Resolve paths relative to the crate manifest so the build works
    // regardless of the caller's CWD. The proto schema and VERSION file
    // live one directory up at the repo root (shared by Go and Rust).
    let manifest_dir = PathBuf::from(env!("CARGO_MANIFEST_DIR"));
    let repo_root = manifest_dir.parent().expect("manifest dir has a parent");

    let proto_file = repo_root.join("proto").join("graywolf.proto");
    let proto_dir = repo_root.join("proto");
    let version_file = repo_root.join("VERSION");

    println!("cargo:rerun-if-changed={}", proto_file.display());
    prost_build::Config::new()
        .compile_protos(&[&proto_file], &[&proto_dir])
        .expect("failed to compile graywolf.proto");

    // Inject version from GRAYWOLF_VERSION env (set by Makefile / CI),
    // falling back to the VERSION file at the repo root.
    println!("cargo:rerun-if-env-changed=GRAYWOLF_VERSION");
    println!("cargo:rerun-if-changed={}", version_file.display());
    let version = std::env::var("GRAYWOLF_VERSION").unwrap_or_else(|_| {
        std::fs::read_to_string(&version_file)
            .map(|s| s.trim().to_string())
            .unwrap_or_else(|_| "dev".to_string())
    });
    println!("cargo:rustc-env=GRAYWOLF_VERSION={}", version);

    // Inject commit hash from GRAYWOLF_GIT_COMMIT env (set by Makefile / CI),
    // falling back to `git rev-parse --short HEAD` plus a -dirty suffix if
    // the working tree is modified. Both sides of the build (Go + Rust)
    // must format their display string the same way for the startup-time
    // version banner to compare equal: "v{version}-{commit}".
    println!("cargo:rerun-if-env-changed=GRAYWOLF_GIT_COMMIT");
    let commit = std::env::var("GRAYWOLF_GIT_COMMIT")
        .ok()
        .filter(|s| !s.is_empty())
        .unwrap_or_else(|| derive_commit(repo_root));
    println!("cargo:rustc-env=GRAYWOLF_GIT_COMMIT={}", commit);
}

fn derive_commit(repo_root: &Path) -> String {
    let short = Command::new("git")
        .args(["rev-parse", "--short", "HEAD"])
        .current_dir(repo_root)
        .output()
        .ok()
        .filter(|o| o.status.success())
        .map(|o| String::from_utf8_lossy(&o.stdout).trim().to_string())
        .filter(|s| !s.is_empty())
        .unwrap_or_else(|| "unknown".to_string());

    let dirty = Command::new("git")
        .args(["diff-index", "--quiet", "HEAD", "--"])
        .current_dir(repo_root)
        .status()
        .ok()
        .map(|s| !s.success())
        .unwrap_or(false);

    if dirty {
        format!("{}-dirty", short)
    } else {
        short
    }
}
