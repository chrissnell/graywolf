//! Generates Rust bindings from `../proto/graywolf.proto` via prost-build.
//! The output is included by `src/ipc/proto.rs`.

use std::path::PathBuf;

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
}
