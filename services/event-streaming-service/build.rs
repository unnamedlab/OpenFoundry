//! Build script: compile the router gRPC contract with tonic-build.
//!
//! We rely on `protoc-bin-vendored` so the build does not require `protoc` to
//! be installed on the host (CI / sandbox friendly).

fn main() -> Result<(), Box<dyn std::error::Error>> {
    let protoc = protoc_bin_vendored::protoc_bin_path()?;
    // Tell prost-build / tonic-build where the vendored protoc lives.
    // SAFETY: build scripts run single-threaded; setting an env var here is
    // standard practice for protobuf builds.
    unsafe {
        std::env::set_var("PROTOC", protoc);
    }

    let proto_root = std::path::PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .join("..")
        .join("..")
        .join("proto");
    let router_proto = proto_root.join("streaming").join("router.proto");

    println!("cargo:rerun-if-changed={}", router_proto.display());

    tonic_build::configure()
        .build_server(true)
        .build_client(false)
        .compile_protos(&[router_proto], &[proto_root])?;

    Ok(())
}
