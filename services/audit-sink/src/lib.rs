//! `audit-sink` — `audit.events.v1` Kafka topic → Iceberg
//! `of.audit.events`.
//!
//! ## Scope
//!
//! This crate owns:
//!
//! 1. Wire format of an `audit.events.v1` record (the same envelope
//!    `identity-federation-service::hardening::audit_topic` uses).
//! 2. Batch policy (size + time threshold) — the Iceberg writer
//!    flushes when either is reached.
//! 3. Iceberg target identifiers (catalog / namespace / table /
//!    partition spec).
//! 4. Runtime wiring behind the `runtime` feature: Kafka consume →
//!    Arrow batch → Iceberg `append_record_batches` → Kafka commit.
//!
//! ## Why a separate service
//!
//! `audit.events.v1` retention is operationally infinite (10y in
//! the topic, then trimmed once the sink is the system of record —
//! see `infra/k8s/strimzi/topics-domain-v1.yaml`). The sink owns
//! the conversion to columnar storage and the snapshot retention
//! policy (per S5.1.c: `expire_snapshots` disabled forever).

use serde::{Deserialize, Serialize};
use std::time::Duration;
use thiserror::Error;
use uuid::Uuid;

#[cfg(feature = "runtime")]
pub mod runtime;

/// Iceberg schema, partition, sort and WORM retention pins for
/// `of_audit.events`. See [`iceberg_schema`] for the full constant set
/// (S5.1.a + S5.1.c).
pub mod iceberg_schema;

/// Source topic. Pinned here so a typo at wiring time is a compile
/// error.
pub const SOURCE_TOPIC: &str = "audit.events.v1";

/// Consumer group. One group across replicas — Kafka rebalance
/// distributes partitions.
pub const CONSUMER_GROUP: &str = "audit-sink";

/// Iceberg target — catalog / namespace / table.
pub mod iceberg_target {
    /// Lakekeeper REST catalog (per ADR-0026).
    pub const CATALOG: &str = "lakekeeper";
    pub const NAMESPACE: &str = "of_audit";
    pub const TABLE: &str = "events";
    /// Partition spec: by `day` derived from `at`. Sort order
    /// inside a partition is `at`.
    pub const PARTITION_TRANSFORM: &str = "day(at)";
    pub const SORT_ORDER: &str = "at ASC";
}

/// Audit-event envelope as it lands on Kafka. Mirrors
/// `identity-federation-service::hardening::audit_topic::AuditEnvelope`
/// — kept structural rather than `pub use` to avoid a service ↔
/// service Cargo dependency.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct AuditEnvelope {
    pub event_id: Uuid,
    /// Unix epoch microseconds. The partition transform is
    /// `day(at)` so this is the partition key.
    pub at: i64,
    pub correlation_id: Option<String>,
    pub kind: String,
    pub payload: serde_json::Value,
}

#[derive(Debug, Error)]
pub enum DecodeError {
    #[error("invalid JSON payload: {0}")]
    Json(#[from] serde_json::Error),
}

pub fn decode(bytes: &[u8]) -> Result<AuditEnvelope, DecodeError> {
    Ok(serde_json::from_slice(bytes)?)
}

/// Batch policy. Plan calls for **either** 100k records **or** 60s,
/// whichever comes first.
#[derive(Debug, Clone, Copy)]
pub struct BatchPolicy {
    pub max_records: usize,
    pub max_wait: Duration,
}

impl BatchPolicy {
    pub const PLAN_DEFAULT: BatchPolicy = BatchPolicy {
        max_records: 100_000,
        max_wait: Duration::from_secs(60),
    };

    pub fn should_flush(&self, current_records: usize, elapsed: Duration) -> bool {
        current_records >= self.max_records || elapsed >= self.max_wait
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn decodes_envelope() {
        let bytes = serde_json::to_vec(&json!({
            "event_id": "00000000-0000-7000-8000-000000000001",
            "at": 1_700_000_000_000_000_i64,
            "correlation_id": "abc",
            "kind": "Login",
            "payload": { "user_id": "u1", "outcome": "success" },
        }))
        .unwrap();
        let env = decode(&bytes).unwrap();
        assert_eq!(env.kind, "Login");
        assert_eq!(env.at, 1_700_000_000_000_000);
    }

    #[test]
    fn batch_flushes_on_size() {
        let p = BatchPolicy::PLAN_DEFAULT;
        assert!(p.should_flush(100_000, Duration::from_secs(1)));
        assert!(!p.should_flush(99_999, Duration::from_secs(1)));
    }

    #[test]
    fn batch_flushes_on_time() {
        let p = BatchPolicy::PLAN_DEFAULT;
        assert!(p.should_flush(0, Duration::from_secs(60)));
        assert!(!p.should_flush(0, Duration::from_secs(59)));
    }

    #[test]
    fn iceberg_target_pinned() {
        assert_eq!(iceberg_target::TABLE, "events");
        assert_eq!(iceberg_target::PARTITION_TRANSFORM, "day(at)");
    }
}
