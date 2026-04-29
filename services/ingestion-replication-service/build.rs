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
}
