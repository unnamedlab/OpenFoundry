//! Compiles the media-set proto contracts into Rust types + tonic
//! server/client stubs that the gRPC service in `src/grpc.rs` and the
//! REST handlers' DTO conversions both consume.
//!
//! The include root is the workspace-level `proto/` directory so imports
//! like `import "common/types.proto";` resolve without re-rooting.

fn main() -> Result<(), Box<dyn std::error::Error>> {
    let proto_root = "../../proto";
    let media_set = "../../proto/media_set/media_set.proto";
    let media_set_service = "../../proto/media_set/media_set_service.proto";
    let access_pattern = "../../proto/media_set/access_pattern.proto";
    let common_types = "../../proto/common/types.proto";

    println!("cargo:rerun-if-changed={media_set}");
    println!("cargo:rerun-if-changed={media_set_service}");
    println!("cargo:rerun-if-changed={access_pattern}");
    println!("cargo:rerun-if-changed={common_types}");

    tonic_build::configure()
        .build_server(true)
        .build_client(true)
        .compile_protos(
            &[media_set, media_set_service, access_pattern, common_types],
            &[proto_root],
        )?;

    Ok(())
}
