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
