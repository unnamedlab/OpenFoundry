//! Kafka subscriber for `foundry.branch.events.v1`.
//!
//! Two responsibilities:
//!
//!   1. Auto-link branches whose creation event carries the
//!      `global_branch=<rid>` label. The label is the canonical
//!      hand-off mechanism between plane-local creators and the
//!      global-branching plane (see Foundry "Branching lifecycle").
//!   2. Update link statuses on archive / restore / delete events:
//!      `archived` → status `archived`, `restored` → `in_sync`,
//!      `deleted` → drop the link.
//!
//! ## Why this lives in a separate module
//!
//! The event handler is dependency-injected through the
//! [`SubscriberPort`] trait so unit tests can drive it with a
//! synthetic `BranchEnvelope` JSON payload — no Kafka cluster
//! required. The Kafka glue (`subscribe_loop`) wraps the trait
//! implementation in a real consumer; tests bypass it.

use async_trait::async_trait;
use serde_json::Value;
use sqlx::PgPool;
use uuid::Uuid;

use super::store;

#[derive(Debug, thiserror::Error)]
pub enum SubscriberError {
    #[error("payload missing required field {0}")]
    MissingField(&'static str),
    #[error("payload field {0} is malformed")]
    Malformed(&'static str),
    #[error("db error: {0}")]
    Db(#[from] sqlx::Error),
}

#[async_trait]
pub trait SubscriberPort: Send + Sync {
    async fn handle(&self, event: &Value) -> Result<(), SubscriberError>;
}

/// Default port — Postgres-backed.
pub struct PostgresSubscriber {
    pub pool: PgPool,
}

#[async_trait]
impl SubscriberPort for PostgresSubscriber {
    async fn handle(&self, event: &Value) -> Result<(), SubscriberError> {
        let event_type = event
            .get("event_type")
            .and_then(Value::as_str)
            .ok_or(SubscriberError::MissingField("event_type"))?;
        let branch_rid = event
            .get("branch_rid")
            .and_then(Value::as_str)
            .ok_or(SubscriberError::MissingField("branch_rid"))?;

        match event_type {
            "dataset.branch.created.v1" => {
                if let Some(global_rid) = label_value(event, "global_branch") {
                    let global_id = parse_global_rid(&global_rid)
                        .ok_or(SubscriberError::Malformed("labels.global_branch"))?;
                    let dataset_rid = event
                        .get("dataset_rid")
                        .and_then(Value::as_str)
                        .ok_or(SubscriberError::MissingField("dataset_rid"))?;
                    let request = super::model::CreateGlobalBranchLinkRequest {
                        resource_type: "dataset".into(),
                        resource_rid: dataset_rid.to_string(),
                        branch_rid: branch_rid.to_string(),
                    };
                    store::add_link(&self.pool, global_id, &request).await?;
                }
            }
            "dataset.branch.archived.v1" => {
                store::update_links_for_branch(&self.pool, branch_rid, "archived").await?;
            }
            "dataset.branch.restored.v1" => {
                store::update_links_for_branch(&self.pool, branch_rid, "in_sync").await?;
            }
            "dataset.branch.reparented.v1" | "dataset.branch.markings.updated.v1" => {
                store::update_links_for_branch(&self.pool, branch_rid, "drifted").await?;
            }
            _ => {
                // Pass-through for events the global plane doesn't
                // currently care about (created without label,
                // retention.updated, deleted). Logged at debug.
                tracing::debug!(%event_type, %branch_rid, "ignored");
            }
        }
        Ok(())
    }
}

fn label_value(event: &Value, key: &str) -> Option<String> {
    event
        .get("labels")
        .and_then(Value::as_object)
        .and_then(|m| m.get(key))
        .and_then(Value::as_str)
        .map(str::to_string)
}

const GLOBAL_RID_PREFIX: &str = "ri.foundry.main.globalbranch.";

fn parse_global_rid(rid_or_uuid: &str) -> Option<Uuid> {
    let trimmed = rid_or_uuid.trim();
    if let Some(tail) = trimmed.strip_prefix(GLOBAL_RID_PREFIX) {
        Uuid::parse_str(tail).ok()
    } else {
        Uuid::parse_str(trimmed).ok()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parse_global_rid_accepts_rid_or_bare_uuid() {
        let id = Uuid::new_v4();
        let rid = format!("{GLOBAL_RID_PREFIX}{id}");
        assert_eq!(parse_global_rid(&rid), Some(id));
        assert_eq!(parse_global_rid(&id.to_string()), Some(id));
    }

    #[test]
    fn label_value_extracts_global_branch_label() {
        let event = serde_json::json!({"labels": {"global_branch": "GB-123"}});
        assert_eq!(label_value(&event, "global_branch"), Some("GB-123".into()));
    }

    #[test]
    fn label_value_returns_none_when_labels_missing() {
        assert!(label_value(&serde_json::json!({}), "global_branch").is_none());
    }
}
