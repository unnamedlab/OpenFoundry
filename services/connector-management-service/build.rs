//! Compile the `IngestionControlPlane` proto (client stub for gRPC calls
//! to `ingestion-replication-service` from `run_sync`) plus the
//! `VirtualTableCatalog` proto absorbed from the retired
//! `virtual-table-service` per ADR-0030 (S8 / B17). The virtual-table
//! proto lives under `proto/virtual_tables/` in this crate.

fn main() -> Result<(), Box<dyn std::error::Error>> {
    let protoc = protoc_bin_vendored::protoc_bin_path()?;
    // SAFETY: build scripts run single-threaded.
    unsafe {
        std::env::set_var("PROTOC", protoc);
    }

    let ingestion_proto = "../ingestion-replication-service/proto/ingestion_control_plane.proto";
    println!("cargo:rerun-if-changed={ingestion_proto}");
    tonic_build::configure()
        .build_server(false)
        .build_client(true)
        .compile_protos(&[ingestion_proto], &["../ingestion-replication-service/proto"])?;

    // Virtual-tables proto absorbed from virtual-table-service.
    let vt_proto_dir =
        std::path::PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("proto/virtual_tables");
    let vt_proto_file = vt_proto_dir.join("virtual_tables.proto");
    println!("cargo:rerun-if-changed={}", vt_proto_file.display());
    tonic_build::configure()
        .build_server(true)
        .build_client(true)
        .compile_protos(&[vt_proto_file], &[vt_proto_dir])?;

    Ok(())
}
