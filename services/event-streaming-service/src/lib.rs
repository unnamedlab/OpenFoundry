//! `event-streaming-service` library root.
//!
//! Exposes the building blocks of the routing facade so they can be exercised
//! both from the binary and from the unit-test harness.

pub mod app_config;
pub mod backends;
pub mod grpc;
pub mod metrics;
pub mod router;

/// Generated gRPC bindings for the router service.
pub mod proto {
    pub mod router {
        // tonic_build emits files named after the protobuf package.
        tonic::include_proto!("openfoundry.streaming.router.v1");
    }
}
