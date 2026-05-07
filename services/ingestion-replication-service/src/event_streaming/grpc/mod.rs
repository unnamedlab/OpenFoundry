//! gRPC layer: implements the `EventRouter` service generated from
//! `proto/streaming/router.proto`.

pub mod router_service;

pub use router_service::EventRouterService;
