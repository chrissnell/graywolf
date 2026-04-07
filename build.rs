//! Generates Rust bindings from `proto/graywolf.proto` via prost-build.
//! The output is included by `src/ipc/proto.rs`.

fn main() {
    println!("cargo:rerun-if-changed=proto/graywolf.proto");
    prost_build::Config::new()
        .compile_protos(&["proto/graywolf.proto"], &["proto/"])
        .expect("failed to compile graywolf.proto");

    // Inject version from GRAYWOLF_VERSION env (set by Makefile / CI),
    // falling back to the VERSION file at the repo root.
    println!("cargo:rerun-if-env-changed=GRAYWOLF_VERSION");
    println!("cargo:rerun-if-changed=VERSION");
    let version = std::env::var("GRAYWOLF_VERSION").unwrap_or_else(|_| {
        std::fs::read_to_string("VERSION")
            .map(|s| s.trim().to_string())
            .unwrap_or_else(|_| "dev".to_string())
    });
    println!("cargo:rustc-env=GRAYWOLF_VERSION={}", version);
}
