//! `retention.sweep` saga — single-step example used by the FASE 6
//! / Tarea 6.3 deliverable and exercised by the chaos test in
//! Tarea 6.4.
//!
//! Today the body is a pure stub (it returns the input untouched).
//! When the retention business logic lands in
//! `services/audit-compliance-service` the [`EvictRetentionEligible`]
//! step will gain an HTTP call to the compliance service's
//! `POST /api/v1/audit/retention/sweep` endpoint. Keeping the body
//! pure today lets the saga runtime be fully integration-tested
//! without depending on an upstream service that does not yet exist.

use async_trait::async_trait;
use saga::{SagaError, SagaStep};
use serde::{Deserialize, Serialize};

/// Inbound payload for `retention.sweep`. Carried verbatim on the
/// `saga.step.requested.v1` event's `input` field.
#[derive(Clone, Debug, Serialize, Deserialize, PartialEq, Eq)]
pub struct RetentionSweepInput {
    /// Owning tenant. Sweeps are tenant-scoped.
    pub tenant_id: String,
    /// Sweep cut-off — records older than this many days are
    /// candidates for eviction.
    #[serde(default = "default_days")]
    pub older_than_days: u32,
    /// Optional dry-run flag. When `true` the step body counts the
    /// candidate records but does not delete; the output reports the
    /// hypothetical eviction count.
    #[serde(default)]
    pub dry_run: bool,
}

fn default_days() -> u32 {
    90
}

/// Outbound projection emitted on `saga.step.completed.v1` for this
/// step.
#[derive(Clone, Debug, Serialize, Deserialize, PartialEq, Eq)]
pub struct RetentionSweepOutput {
    /// How many records the step deleted (or, on `dry_run`, would
    /// have deleted).
    pub evicted: u64,
    /// Echo of the input cut-off, kept on the event so downstream
    /// consumers do not need to join back to the originating
    /// saga payload.
    pub older_than_days: u32,
    /// Echo of the dry-run flag.
    pub dry_run: bool,
}

/// Single step of the `retention.sweep` saga.
///
/// **TODO (FASE 9 / future task)**: replace the stub body with an
/// HTTP call to `audit-compliance-service::POST /api/v1/audit/`
/// `retention/sweep` once that endpoint exists. The output shape
/// already mirrors the eventual response (`evicted` count, etc.).
pub struct EvictRetentionEligible;

#[async_trait]
impl SagaStep for EvictRetentionEligible {
    type Input = RetentionSweepInput;
    type Output = RetentionSweepOutput;

    fn step_name() -> &'static str {
        "evict_retention_eligible"
    }

    async fn execute(input: Self::Input) -> Result<Self::Output, SagaError> {
        // Pure stub — the runtime contract is satisfied (the step
        // returns Ok, the runner advances). When the upstream
        // endpoint exists, replace this body with a reqwest::Client
        // invocation that translates the response into
        // RetentionSweepOutput.
        Ok(RetentionSweepOutput {
            evicted: 0,
            older_than_days: input.older_than_days,
            dry_run: input.dry_run,
        })
    }

    async fn compensate(_input: Self::Input) -> Result<(), SagaError> {
        // Single-step saga; nothing to compensate. A failed
        // `execute` lands the saga directly in `failed` (no
        // compensations are recorded if the first step never
        // succeeded).
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn input_round_trip_with_defaults() {
        let raw = json!({"tenant_id": "acme"});
        let parsed: RetentionSweepInput = serde_json::from_value(raw).unwrap();
        assert_eq!(parsed.older_than_days, 90);
        assert!(!parsed.dry_run);
    }

    #[test]
    fn input_round_trip_explicit() {
        let parsed: RetentionSweepInput = serde_json::from_value(json!({
            "tenant_id": "acme",
            "older_than_days": 30,
            "dry_run": true,
        }))
        .unwrap();
        assert_eq!(parsed.older_than_days, 30);
        assert!(parsed.dry_run);
    }

    #[tokio::test]
    async fn execute_echoes_input() {
        let out = EvictRetentionEligible::execute(RetentionSweepInput {
            tenant_id: "acme".into(),
            older_than_days: 60,
            dry_run: true,
        })
        .await
        .unwrap();
        assert_eq!(out.older_than_days, 60);
        assert_eq!(out.evicted, 0);
        assert!(out.dry_run);
    }

    #[test]
    fn step_name_is_pinned() {
        assert_eq!(
            EvictRetentionEligible::step_name(),
            "evict_retention_eligible"
        );
    }
}
