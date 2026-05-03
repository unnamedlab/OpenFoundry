//! `EXPORT` job runner — Foundry "Data Connection Exports".
//!
//! Pushes the resolved input view to an external destination (S3 /
//! GCS / HTTP / JDBC). Export jobs typically have *zero* output
//! datasets because the data leaves Foundry; the runner reflects
//! that by always producing a synthetic content hash that captures
//! the destination + payload identity.
//!
//! The actual byte streaming is delegated to a generic HTTP target —
//! the runner POSTs a manifest describing the export to the
//! configured endpoint. Connector-specific transports (signed S3
//! requests, JDBC writers) live in `connector-management-service`
//! and are out of scope here.

use async_trait::async_trait;
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};

use crate::domain::build_executor::{JobContext, JobOutcome, JobRunner};

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
pub enum ExportTarget {
    S3,
    Gcs,
    Http,
    Jdbc,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ExportConfig {
    pub export_target: ExportTarget,
    /// Endpoint URL (S3 bucket, HTTP webhook, JDBC connection string).
    pub endpoint: String,
    /// Free-form transport options (region, prefix, auth aliases, …).
    /// Forwarded as the body of the manifest POST so the receiving
    /// connector can validate them against its ACL.
    #[serde(default)]
    pub options: serde_json::Value,
    /// Source dataset to export. The runner does not enforce that
    /// `source_dataset_rid` is one of the resolved inputs — that is
    /// validated at resolve-time in [`super::validate_logic_kind`].
    pub source_dataset_rid: String,
    /// Optional ACL alias the receiving connector must honour.
    #[serde(default)]
    pub acl_alias: Option<String>,
}

pub struct ExportJobRunner {
    http: reqwest::Client,
}

impl ExportJobRunner {
    pub fn new(http: reqwest::Client) -> Self {
        Self { http }
    }
}

#[async_trait]
impl JobRunner for ExportJobRunner {
    async fn run(&self, ctx: &JobContext) -> JobOutcome {
        let cfg: ExportConfig = match serde_json::from_value(ctx.job_spec.logic_payload.clone()) {
            Ok(c) => c,
            Err(err) => {
                return JobOutcome::Failed {
                    reason: format!("invalid EXPORT payload: {err}"),
                };
            }
        };

        if cfg.acl_alias.is_none() {
            // Foundry doc requires an ACL alias for any external push;
            // refuse early so unconfigured exports can't quietly leak
            // data.
            return JobOutcome::Failed {
                reason: "EXPORT job missing acl_alias: refusing to push to unconfigured target"
                    .into(),
            };
        }

        let manifest = serde_json::json!({
            "export_target": serde_json::to_value(&cfg.export_target).unwrap_or(serde_json::Value::Null),
            "endpoint": cfg.endpoint,
            "options": cfg.options,
            "source_dataset_rid": cfg.source_dataset_rid,
            "acl_alias": cfg.acl_alias,
            "build_branch": ctx.build_branch,
            "job_rid": ctx.job_spec.rid,
        });

        tracing::info!(
            target: "audit",
            actor = ctx.build_branch.as_str(),
            action = "export.dispatched",
            target = ?cfg.export_target,
            endpoint = cfg.endpoint.as_str(),
            "EXPORT runner pushing to external destination"
        );

        let resp = match self
            .http
            .post(&cfg.endpoint)
            .json(&manifest)
            .send()
            .await
        {
            Ok(r) => r,
            Err(err) => {
                return JobOutcome::Failed {
                    reason: format!("export endpoint unreachable: {err}"),
                };
            }
        };
        if !resp.status().is_success() {
            let status = resp.status();
            let body = resp.text().await.unwrap_or_default();
            return JobOutcome::Failed {
                reason: format!("export target returned {status}: {body}"),
            };
        }

        let mut hasher = Sha256::new();
        hasher.update(b"export");
        hasher.update(cfg.endpoint.as_bytes());
        hasher.update(b"|");
        hasher.update(cfg.source_dataset_rid.as_bytes());
        let hash = format!("{:x}", hasher.finalize());
        JobOutcome::Completed {
            output_content_hash: hash,
        }
    }
}
