//! Parameterized pipelines store + domain rules.
//!
//! Per the Foundry doc § "Parameterized pipelines":
//!
//!   * The same DAG runs once per *deployment*, with each deployment
//!     overriding a fixed parameter (the `deployment_key_param`).
//!   * Outputs from every deployment are unioned via a "Views"
//!     dataset that stamps each row with `_deployment_key`.
//!   * "Automated triggers are not yet supported." — every run is a
//!     manual dispatch, so the run handler rejects schedules whose
//!     trigger is anything other than manual.
//!
//! This module is pure-data: the row shapes plus a [`TriggerKind`]
//! discriminator the run handler uses to enforce the manual-only
//! rule. The actual SQL is grouped at the bottom in `pg`-feature-
//! gated functions.

use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use serde_json::{Map, Value};
use thiserror::Error;
use uuid::Uuid;

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct ParameterizedPipeline {
    pub id: Uuid,
    pub pipeline_rid: String,
    pub deployment_key_param: String,
    pub output_dataset_rids: Vec<String>,
    pub union_view_dataset_rid: String,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct PipelineDeployment {
    pub id: Uuid,
    pub parameterized_pipeline_id: Uuid,
    pub deployment_key: String,
    pub parameter_values: Map<String, Value>,
    pub created_by: String,
    pub created_at: DateTime<Utc>,
}

/// What the dispatcher knows about a schedule's trigger when deciding
/// whether to allow a parameterized run. The Foundry doc forbids any
/// automated dispatch on a parameterized pipeline; only Manual is
/// permitted.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum TriggerKind {
    Manual,
    Time,
    Event,
    Compound,
}

#[derive(Debug, Error, PartialEq, Eq)]
pub enum DispatchError {
    #[error("parameterized pipelines reject automated triggers; only manual dispatch is allowed")]
    AutomatedTriggerRejected,
    #[error("deployment_key value '{0}' does not match the deployment_key_param '{1}'")]
    DeploymentKeyMismatch(String, String),
}

/// Enforce the doc's "manual only" rule. Returns Ok for Manual,
/// rejects every other variant.
pub fn assert_manual_dispatch(trigger: TriggerKind) -> Result<(), DispatchError> {
    match trigger {
        TriggerKind::Manual => Ok(()),
        _ => Err(DispatchError::AutomatedTriggerRejected),
    }
}

/// Validate that the deployment row's `parameter_values` carry a value
/// for `deployment_key_param` matching the deployment_key. The build
/// pass relies on this invariant when stamping per-row deployment_key
/// onto each output transaction.
pub fn assert_deployment_key_consistent(
    pipeline: &ParameterizedPipeline,
    deployment: &PipelineDeployment,
) -> Result<(), DispatchError> {
    let recorded = deployment
        .parameter_values
        .get(&pipeline.deployment_key_param)
        .and_then(Value::as_str)
        .unwrap_or("");
    if recorded == deployment.deployment_key {
        Ok(())
    } else {
        Err(DispatchError::DeploymentKeyMismatch(
            recorded.to_string(),
            pipeline.deployment_key_param.clone(),
        ))
    }
}

#[cfg(feature = "pg")]
pub mod pg {
    //! Postgres-backed CRUD. Gated so the lib can compile without
    //! pulling sqlx into pure-logic test binaries.

    use super::*;
    use sqlx::{PgPool, Row, postgres::PgRow};

    #[derive(Debug, Error)]
    pub enum StoreError {
        #[error("database error: {0}")]
        Db(#[from] sqlx::Error),
        #[error("parameterized pipeline '{0}' not found")]
        NotFound(String),
    }

    pub async fn create_parameterized(
        pool: &PgPool,
        pipeline_rid: &str,
        deployment_key_param: &str,
        output_dataset_rids: &[String],
        union_view_dataset_rid: &str,
    ) -> Result<ParameterizedPipeline, StoreError> {
        let id = Uuid::now_v7();
        let row = sqlx::query(
            r#"INSERT INTO parameterized_pipelines
                    (id, pipeline_rid, deployment_key_param,
                     output_dataset_rids, union_view_dataset_rid)
                VALUES ($1, $2, $3, $4, $5)
                RETURNING id, pipeline_rid, deployment_key_param,
                          output_dataset_rids, union_view_dataset_rid,
                          created_at, updated_at"#,
        )
        .bind(id)
        .bind(pipeline_rid)
        .bind(deployment_key_param)
        .bind(output_dataset_rids)
        .bind(union_view_dataset_rid)
        .fetch_one(pool)
        .await?;
        Ok(parameterized_from_row(&row)?)
    }

    pub async fn get_by_pipeline_rid(
        pool: &PgPool,
        pipeline_rid: &str,
    ) -> Result<Option<ParameterizedPipeline>, StoreError> {
        let row = sqlx::query(
            r#"SELECT id, pipeline_rid, deployment_key_param,
                      output_dataset_rids, union_view_dataset_rid,
                      created_at, updated_at
                 FROM parameterized_pipelines WHERE pipeline_rid = $1"#,
        )
        .bind(pipeline_rid)
        .fetch_optional(pool)
        .await?;
        match row {
            Some(r) => Ok(Some(parameterized_from_row(&r)?)),
            None => Ok(None),
        }
    }

    pub async fn create_deployment(
        pool: &PgPool,
        parameterized_pipeline_id: Uuid,
        deployment_key: &str,
        parameter_values: &Map<String, Value>,
        created_by: &str,
    ) -> Result<PipelineDeployment, StoreError> {
        let id = Uuid::now_v7();
        let row = sqlx::query(
            r#"INSERT INTO pipeline_deployments
                    (id, parameterized_pipeline_id, deployment_key,
                     parameter_values, created_by)
                VALUES ($1, $2, $3, $4, $5)
                RETURNING id, parameterized_pipeline_id, deployment_key,
                          parameter_values, created_by, created_at"#,
        )
        .bind(id)
        .bind(parameterized_pipeline_id)
        .bind(deployment_key)
        .bind(Value::Object(parameter_values.clone()))
        .bind(created_by)
        .fetch_one(pool)
        .await?;
        Ok(deployment_from_row(&row)?)
    }

    pub async fn list_deployments(
        pool: &PgPool,
        parameterized_pipeline_id: Uuid,
    ) -> Result<Vec<PipelineDeployment>, StoreError> {
        let rows = sqlx::query(
            r#"SELECT id, parameterized_pipeline_id, deployment_key,
                      parameter_values, created_by, created_at
                 FROM pipeline_deployments
                WHERE parameterized_pipeline_id = $1
                ORDER BY created_at DESC"#,
        )
        .bind(parameterized_pipeline_id)
        .fetch_all(pool)
        .await?;
        rows.iter()
            .map(deployment_from_row)
            .collect::<Result<Vec<_>, _>>()
            .map_err(StoreError::from)
    }

    pub async fn delete_deployment(pool: &PgPool, id: Uuid) -> Result<(), StoreError> {
        sqlx::query("DELETE FROM pipeline_deployments WHERE id = $1")
            .bind(id)
            .execute(pool)
            .await?;
        Ok(())
    }

    fn parameterized_from_row(row: &PgRow) -> Result<ParameterizedPipeline, sqlx::Error> {
        Ok(ParameterizedPipeline {
            id: row.try_get("id")?,
            pipeline_rid: row.try_get("pipeline_rid")?,
            deployment_key_param: row.try_get("deployment_key_param")?,
            output_dataset_rids: row.try_get("output_dataset_rids")?,
            union_view_dataset_rid: row.try_get("union_view_dataset_rid")?,
            created_at: row.try_get("created_at")?,
            updated_at: row.try_get("updated_at")?,
        })
    }

    fn deployment_from_row(row: &PgRow) -> Result<PipelineDeployment, sqlx::Error> {
        let value: Value = row.try_get("parameter_values")?;
        let parameter_values = match value {
            Value::Object(map) => map,
            _ => Map::new(),
        };
        Ok(PipelineDeployment {
            id: row.try_get("id")?,
            parameterized_pipeline_id: row.try_get("parameterized_pipeline_id")?,
            deployment_key: row.try_get("deployment_key")?,
            parameter_values,
            created_by: row.try_get("created_by")?,
            created_at: row.try_get("created_at")?,
        })
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    fn pipeline_fixture() -> ParameterizedPipeline {
        ParameterizedPipeline {
            id: Uuid::nil(),
            pipeline_rid: "ri.foundry.main.pipeline.alpha".into(),
            deployment_key_param: "region".into(),
            output_dataset_rids: vec!["ri.foundry.main.dataset.alpha-out".into()],
            union_view_dataset_rid: "ri.foundry.main.dataset.alpha-view".into(),
            created_at: Utc::now(),
            updated_at: Utc::now(),
        }
    }

    fn deployment_with_key(
        parent: &ParameterizedPipeline,
        key: &str,
        recorded: &str,
    ) -> PipelineDeployment {
        let mut parameter_values = Map::new();
        parameter_values.insert("region".into(), json!(recorded));
        parameter_values.insert("limit".into(), json!(1000));
        PipelineDeployment {
            id: Uuid::nil(),
            parameterized_pipeline_id: parent.id,
            deployment_key: key.into(),
            parameter_values,
            created_by: "tester".into(),
            created_at: Utc::now(),
        }
    }

    #[test]
    fn assert_manual_dispatch_accepts_manual_only() {
        assert!(assert_manual_dispatch(TriggerKind::Manual).is_ok());
        for kind in [TriggerKind::Time, TriggerKind::Event, TriggerKind::Compound] {
            assert_eq!(
                assert_manual_dispatch(kind),
                Err(DispatchError::AutomatedTriggerRejected)
            );
        }
    }

    #[test]
    fn deployment_key_consistent_when_param_value_matches_key() {
        let p = pipeline_fixture();
        let d = deployment_with_key(&p, "eu-west", "eu-west");
        assert!(assert_deployment_key_consistent(&p, &d).is_ok());
    }

    #[test]
    fn deployment_key_mismatch_returns_error() {
        let p = pipeline_fixture();
        let d = deployment_with_key(&p, "eu-west", "us-east");
        let err = assert_deployment_key_consistent(&p, &d).unwrap_err();
        assert!(matches!(err, DispatchError::DeploymentKeyMismatch(_, _)));
    }
}
