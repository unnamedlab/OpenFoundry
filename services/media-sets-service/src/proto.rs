//! Tonic-generated types for the media-set proto package.
//!
//! Both the gRPC server in [`crate::grpc`] and the REST DTO conversions
//! re-use these directly; the build script in `build.rs` compiles the
//! contracts under `proto/media_set/`.

#![allow(clippy::all, clippy::pedantic, missing_docs, unreachable_pub)]

pub mod media_set {
    tonic::include_proto!("open_foundry.media_set");
}

pub mod common {
    tonic::include_proto!("open_foundry.common");
}
