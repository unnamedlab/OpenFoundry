//! Compile the `IngestionControlPlane` proto so this service can issue gRPC
//! calls to `ingestion-replication-service` from `run_sync`. The .proto file
//! is owned by the ingestion crate; we only generate a client stub here.

fn main() -> Result<(), Box<dyn std::error::Error>> {
    let proto = "../ingestion-replication-service/proto/ingestion_control_plane.proto";
    println!("cargo:rerun-if-changed={proto}");
    tonic_build::configure()
        .build_server(false)
        .build_client(true)
        .compile_protos(
            &[proto],
            &["../ingestion-replication-service/proto"],
        )?;
    Ok(())
}
