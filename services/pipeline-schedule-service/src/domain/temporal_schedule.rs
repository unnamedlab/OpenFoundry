//! Temporal Schedules adapter for `pipeline-schedule-service` (S2.4.b).
//!
//! Wraps [`temporal_client::PipelineScheduleClient`] behind a small
//! request DTO so HTTP handlers stay thin. The CRUD shape on the
//! Postgres side (the **declarative** schedule rows) is not touched
//! here — that is still owned by `domain::schedule`. This module
//! takes care of materialising a Postgres row as a live Temporal
//! Schedule and tearing it down.

use serde::{Deserialize, Serialize};
use temporal_client::{PipelineRunInput, PipelineScheduleClient, Result};
use uuid::Uuid;

/// REST payload for `POST /api/v1/data-integration/schedules/temporal`.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CreateTemporalScheduleRequest {
    /// Stable identifier — must match the Postgres row's primary key
    /// so `delete_schedule` is symmetric.
    pub schedule_id: String,
    pub pipeline_id: Uuid,
    pub tenant_id: String,
    #[serde(default)]
    pub revision: Option<String>,
    /// Cron expressions (Temporal syntax = standard 5-field cron with
    /// optional 6th seconds field).
    pub cron_expressions: Vec<String>,
    /// IANA timezone, e.g. `"Europe/Madrid"`. Defaults to UTC.
    #[serde(default)]
    pub timezone: Option<String>,
    /// Optional payload merged into [`PipelineRunInput::parameters`].
    #[serde(default)]
    pub parameters: serde_json::Value,
    /// Inbound audit-correlation ID propagated as Temporal search
    /// attribute (ADR-0019). Auto-generated if absent.
    #[serde(default)]
    pub audit_correlation_id: Option<Uuid>,
}

/// Pure helper: build the typed [`PipelineRunInput`] from a REST
/// request. Unit-testable without Temporal.
pub fn to_run_input(req: &CreateTemporalScheduleRequest) -> PipelineRunInput {
    let parameters = match &req.parameters {
        serde_json::Value::Object(_) => req.parameters.clone(),
        _ => serde_json::Value::Object(serde_json::Map::new()),
    };
    PipelineRunInput {
        pipeline_id: req.pipeline_id,
        tenant_id: req.tenant_id.clone(),
        revision: req.revision.clone(),
        parameters,
    }
}

pub async fn create_schedule(
    client: &PipelineScheduleClient,
    req: &CreateTemporalScheduleRequest,
) -> Result<()> {
    let input = to_run_input(req);
    let audit = req.audit_correlation_id.unwrap_or_else(Uuid::now_v7);
    client
        .create(
            req.schedule_id.clone(),
            req.cron_expressions.clone(),
            req.timezone.clone(),
            input,
            audit,
        )
        .await
}

pub async fn delete_schedule(
    client: &PipelineScheduleClient,
    schedule_id: &str,
) -> Result<()> {
    client.delete(schedule_id).await
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn run_input_normalises_non_object_parameters() {
        let req = CreateTemporalScheduleRequest {
            schedule_id: "x".into(),
            pipeline_id: Uuid::now_v7(),
            tenant_id: "t".into(),
            revision: None,
            cron_expressions: vec![],
            timezone: None,
            parameters: serde_json::Value::String("not an object".into()),
            audit_correlation_id: None,
        };
        let input = to_run_input(&req);
        assert!(input.parameters.is_object());
    }

    #[test]
    fn run_input_preserves_object_parameters() {
        let req = CreateTemporalScheduleRequest {
            schedule_id: "daily".into(),
            pipeline_id: Uuid::now_v7(),
            tenant_id: "acme".into(),
            revision: Some("v3".into()),
            cron_expressions: vec!["0 6 * * *".into()],
            timezone: Some("Europe/Madrid".into()),
            parameters: serde_json::json!({"limit": 1000}),
            audit_correlation_id: None,
        };
        let input = to_run_input(&req);
        assert_eq!(input.tenant_id, "acme");
        assert_eq!(input.revision.as_deref(), Some("v3"));
        assert_eq!(input.parameters["limit"], 1000);
    }
}
