//! Compiles the `VirtualTableCatalog` gRPC contract with tonic-build.
//!
//! The proto lives next to this crate (`proto/virtual_tables/virtual_tables.proto`)
//! rather than in the workspace `proto/` tree because the schema is
//! co-owned with the migrations and HTTP handlers in this crate. We rely on
//! `protoc-bin-vendored` so the build does not require a host-installed
//! `protoc` (CI / sandbox friendly — same pattern as
//! `services/event-streaming-service/build.rs`).

fn main() -> Result<(), Box<dyn std::error::Error>> {
    let protoc = protoc_bin_vendored::protoc_bin_path()?;
    // SAFETY: build scripts run single-threaded; setting an env var
    // here is the standard prost-build / tonic-build idiom.
    unsafe {
        std::env::set_var("PROTOC", protoc);
    }

    let proto_dir =
        std::path::PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("proto/virtual_tables");
    let proto_file = proto_dir.join("virtual_tables.proto");

    println!("cargo:rerun-if-changed={}", proto_file.display());

    tonic_build::configure()
        .build_server(true)
        .build_client(true)
        .compile_protos(&[proto_file], &[proto_dir])?;

    Ok(())
}
