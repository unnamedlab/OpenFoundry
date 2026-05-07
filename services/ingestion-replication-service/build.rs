//! Build script: compiles the `IngestionControlPlane` proto plus the
//! streaming protos absorbed from the retired `event-streaming-service`
//! per ADR-0030 (S8).
//!
//! The `EventRouter` and `Streams` services from
//! `proto/streaming/{router,streams}.proto` are compiled with both
//! server and client stubs so this crate hosts both sides: server-side
//! for the absorbed `Publish` / `Subscribe` gRPC facade, client-side
//! for the in-process CDC worker that publishes envelopes.

fn main() -> Result<(), Box<dyn std::error::Error>> {
    let protoc = protoc_bin_vendored::protoc_bin_path()?;
    unsafe {
        std::env::set_var("PROTOC", protoc);
    }

    let ingestion_proto = "proto/ingestion_control_plane.proto";
    let router_proto = "../../proto/streaming/router.proto";
    let streams_proto = "../../proto/streaming/streams.proto";
    println!("cargo:rerun-if-changed={ingestion_proto}");
    println!("cargo:rerun-if-changed={router_proto}");
    println!("cargo:rerun-if-changed={streams_proto}");

    tonic_build::configure()
        .build_server(true)
        .build_client(true)
        .type_attribute(".", "#[derive(serde::Serialize, serde::Deserialize)]")
        .compile_protos(&[ingestion_proto], &["proto"])?;

    // Streaming protos: server + client (no serde derive — keeps
    // payloads as raw bytes vectors, matching the original
    // `event-streaming-service` build).
    tonic_build::configure()
        .build_server(true)
        .build_client(true)
        .compile_protos(&[router_proto, streams_proto], &["../../proto"])?;
    Ok(())
}
