//! `notification-alerting-service` — substrate-only library.
//!
//! Adds the structural surface a future `kafka_consumer` module
//! needs to ingest `ontology.object.changed.v1` /
//! `ontology.action.applied.v1` and fan out alert rules.
//!
//! Same substrate-first pattern as S3.2.d / S3.3 — `main.rs` keeps
//! its current bin entry point; per-handler refactor lands as
//! follow-up PRs.

pub mod kafka_consumer;
