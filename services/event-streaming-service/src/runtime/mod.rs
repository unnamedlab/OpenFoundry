//! Runtime backends for streaming topologies.
//!
//! The default `builtin` runtime is implemented under
//! [`crate::domain::engine`]. This module hosts the integrations with
//! external runtimes — currently only Apache Flink via the Kubernetes
//! Operator (Bloque D).

pub mod flink;
