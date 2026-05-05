//! Build script: compiles the `IngestionControlPlane` proto for the new
//! Kubernetes-native control plane. The legacy `data_integration/sync.proto`
//! and `common/types.proto` are intentionally not compiled by this crate
//! (they belong to the legacy skeleton that is no longer wired in).
//!
//! Also compiles the `EventRouter` client stubs from
//! `proto/streaming/router.proto` so the CDC worker can publish envelopes to
//! `event-streaming-service` over gRPC.

fn main() -> Result<(), Box<dyn std::error::Error>> {
    let protoc = protoc_bin_vendored::protoc_bin_path()?;
    unsafe {
        std::env::set_var("PROTOC", protoc);
    }

    let proto = "proto/ingestion_control_plane.proto";
    let router_proto = "../../proto/streaming/router.proto";
    println!("cargo:rerun-if-changed={proto}");
    println!("cargo:rerun-if-changed={router_proto}");
    tonic_build::configure()
        .build_server(true)
        .build_client(true)
        .type_attribute(".", "#[derive(serde::Serialize, serde::Deserialize)]")
        .compile_protos(&[proto], &["proto"])?;

    // Streaming router: client only, no serde derive (keeps payloads as
    // raw bytes vectors).
    tonic_build::configure()
        .build_server(false)
        .build_client(true)
        .compile_protos(&[router_proto], &["../../proto"])?;
    Ok(())
}
