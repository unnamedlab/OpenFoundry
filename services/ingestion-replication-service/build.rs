use std::path::PathBuf;

fn main() {
    let proto_root = PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .join("../../proto");

    tonic_build::configure()
        .build_server(true)
        .build_client(false)
        // Compile the IngestJob service and the common types it imports.
        .compile_protos(
            &[
                proto_root.join("data_integration/sync.proto"),
                proto_root.join("common/types.proto"),
            ],
            &[proto_root.clone()],
        )
        .expect("failed to compile IngestJob proto");
fn main() -> Result<(), Box<dyn std::error::Error>> {
    let proto = "proto/ingestion_control_plane.proto";
    println!("cargo:rerun-if-changed={proto}");
    tonic_build::configure()
        .build_server(true)
        .build_client(true)
        .type_attribute(".", "#[derive(serde::Serialize, serde::Deserialize)]")
        .compile_protos(&[proto], &["proto"])?;
    Ok(())
}
