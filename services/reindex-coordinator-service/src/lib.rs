//! `reindex-coordinator-service` — Kafka-triggered Cassandra reindex
//! coordinator.
//!
//! This service is the FASE 4 / Tarea 4.2 Rust replacement for the
//! Go `workers-go/reindex` Temporal worker (per ADR-0021 and the
//! migration plan in
//! `docs/architecture/migration-plan-foundry-pattern-orchestration.md`).
//! The full Go-→-Rust mapping is in
//! `docs/architecture/refactor/reindex-worker-inventory.md`.
//!
//! ## Topology
//!
//! ```text
//!   ontology.reindex.requested.v1   (input,  ReindexRequestedV1)
//!                  │
//!                  ▼
//!     ┌──────────────────────────┐
//!     │ reindex-coordinator-svc  │ ── Cassandra (objects_by_type / objects_by_id)
//!     │  pg-runtime-config       │      page-by-page via cassandra-kernel
//!     │   .reindex_jobs (cursor) │
//!     └──────────────────────────┘
//!                  │
//!                  ├─ batches  ──▶  ontology.reindex.v1
//!                  │                (consumed by services/ontology-indexer)
//!                  │
//!                  └─ terminal ──▶  ontology.reindex.completed.v1
//!                                   (ReindexCompletedV1)
//! ```
//!
//! ## Crate layout
//!
//! Pure logic compiled on every CI build:
//!
//! * [`event`] — wire format for the request and completion events,
//!   plus the deterministic `event_id` derivation.
//! * [`state`] — [`JobStatus`](state::JobStatus) enum and the
//!   transition validator that enforces the legal moves
//!   (`queued → running → completed|failed|cancelled`).
//! * [`scan`] — pure decoders for the Cassandra row shape and the
//!   batch JSON published to `ontology.reindex.v1`.
//! * [`topics`] — pinned Kafka topic constants.
//!
//! Runtime wiring (gated by the `runtime` feature) lives in
//! [`runtime`]:
//!
//! * Postgres-backed [`state::JobRepo`] over `sqlx::PgPool`.
//! * Cassandra session helpers backed by `cassandra-kernel`.
//! * Kafka consumer loop (`event-bus-data` subscriber) and
//!   producer (`event-bus-data` publisher).
//! * Per-batch idempotency via `libs/idempotency` Postgres backend.
//! * Prometheus metrics + minimal `:9090/metrics` and
//!   `:8080/health` HTTP endpoints.

#![forbid(unsafe_code)]

pub mod event;
pub mod scan;
pub mod state;
pub mod topics;

#[cfg(feature = "runtime")]
pub mod runtime;

pub use event::{ReindexCompletedV1, ReindexRequestedV1, derive_batch_event_id, derive_job_id};
pub use scan::{ReindexRecord, decode_request, encode_batch_record};
pub use state::{JobStatus, StateError};
