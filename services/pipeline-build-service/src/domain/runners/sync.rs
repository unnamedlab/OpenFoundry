//! `SYNC` job runner — Foundry "Data Connection Sync".
//!
//! Delegates the actual ingest to `connector-management-service`'s
//! `POST /api/v1/data-integration/syncs/{sync_id}/run` endpoint,
//! which (a) materialises an `IngestJobSpec`, (b) hands it to the
//! ingestion-replication-service, and (c) returns an `ingest_job` row.
//!
//! The runner block until the connector returns the dispatch result;
//! actual streaming/long-poll is the connector service's problem.

use async_trait::async_trait;
use serde::{Deserialize, Serialize};

use crate::domain::build_executor::{JobContext, JobOutcome, JobRunner};

/// `JobSpec.logic_payload` shape for SYNC jobs.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SyncConfig {
    /// `ri.foundry.main.connector.<uuid>` — looked up by the connector
    /// service to pull credentials + transport config.
    pub source_rid: String,
    /// Pre-published batch sync def UUID (the connector service
    /// already stored the column mapping / WHERE clauses).
    pub sync_def_id: uuid::Uuid,
    /// Optional override of the run-time options published with the
    /// sync def. Forwarded as-is to the connector service.
    #[serde(default)]
    pub overrides: serde_json::Value,
}

pub struct SyncJobRunner {
    base_url: String,
    http: reqwest::Client,
}

impl SyncJobRunner {
    pub fn new(base_url: String, http: reqwest::Client) -> Self {
        Self { base_url, http }
    }
}

#[async_trait]
impl JobRunner for SyncJobRunner {
    async fn run(&self, ctx: &JobContext) -> JobOutcome {
        let cfg: SyncConfig = match serde_json::from_value(ctx.job_spec.logic_payload.clone()) {
            Ok(c) => c,
            Err(err) => {
                return JobOutcome::Failed {
                    reason: format!("invalid SYNC payload: {err}"),
                };
            }
        };

        let url = format!(
            "{}/api/v1/data-integration/syncs/{}/run",
            self.base_url.trim_end_matches('/'),
            cfg.sync_def_id
        );
        tracing::info!(
            target: "audit",
            actor = ctx.build_branch.as_str(),
            action = "sync.dispatched",
            source_rid = cfg.source_rid.as_str(),
            sync_def_id = %cfg.sync_def_id,
            job_rid = ctx.job_spec.rid.as_str(),
            "SYNC runner dispatched ingest"
        );

        let resp = match self
            .http
            .post(&url)
            .json(&serde_json::json!({
                "overrides": cfg.overrides,
                "build_job_rid": ctx.job_spec.rid,
            }))
            .send()
            .await
        {
            Ok(r) => r,
            Err(err) => {
                return JobOutcome::Failed {
                    reason: format!("connector dispatch failed: {err}"),
                };
            }
        };

        if !resp.status().is_success() {
            let status = resp.status();
            let body = resp.text().await.unwrap_or_default();
            return JobOutcome::Failed {
                reason: format!("connector returned {status}: {body}"),
            };
        }

        let payload: serde_json::Value = resp.json().await.unwrap_or(serde_json::json!({}));
        let hash = payload
            .get("ingest_job_id")
            .and_then(|v| v.as_str())
            .map(|s| format!("sync:{s}"))
            .unwrap_or_else(|| format!("sync:{}", cfg.sync_def_id));

        JobOutcome::Completed {
            output_content_hash: hash,
        }
    }
}
