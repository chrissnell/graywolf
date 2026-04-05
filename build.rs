//! Generates Rust bindings from `proto/graywolf.proto` via prost-build.
//! The output is included by `src/ipc/proto.rs`.

fn main() {
    println!("cargo:rerun-if-changed=proto/graywolf.proto");
    prost_build::Config::new()
        .compile_protos(&["proto/graywolf.proto"], &["proto/"])
        .expect("failed to compile graywolf.proto");
}
