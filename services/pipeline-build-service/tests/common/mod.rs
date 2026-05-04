//! Shared harness for the lifecycle integration tests.
//!
//! Brings up an ephemeral Postgres via `testcontainers`, applies the
//! pipeline-build-service migrations, and exposes mock implementations
//! of [`DatasetVersioningClient`] / [`JobSpecRepo`] so individual
//! tests can drive [`resolve_build`] deterministically.

#![allow(dead_code)]

use std::collections::HashMap;
use std::sync::{Arc, Mutex};
use std::time::Duration;

use async_trait::async_trait;
use sqlx::PgPool;
use testcontainers::{ContainerAsync, GenericImage};
use testing::containers::boot_postgres;

use pipeline_build_service::domain::build_executor::{
    JobContext, JobOutcome, JobRunner, OutputClientError, OutputTransactionClient,
};
use pipeline_build_service::domain::build_resolution::{
    BranchSnapshot, ClientError, DatasetVersioningClient, JobSpec, JobSpecRepo,
};

pub struct Harness {
    pub container: ContainerAsync<GenericImage>,
    pub pool: PgPool,
}

pub async fn spawn() -> Harness {
    let (container, pool, _url) = boot_postgres().await;
    sqlx::migrate!("./migrations")
        .run(&pool)
        .await
        .expect("apply pipeline-build-service migrations");
    Harness { container, pool }
}

// --------------------------------------------------------------------- mocks

#[derive(Default)]
pub struct MockDatasetClient {
    pub branches: Mutex<HashMap<String, Vec<BranchSnapshot>>>,
    pub schemas: Mutex<HashMap<(String, String), serde_json::Value>>,
    pub txn_counter: Mutex<u64>,
    pub fail_open_for: Mutex<Option<String>>,
}

impl MockDatasetClient {
    pub fn add_branch(&self, dataset_rid: &str, branch: BranchSnapshot) {
        self.branches
            .lock()
            .unwrap()
            .entry(dataset_rid.to_string())
            .or_default()
            .push(branch);
    }

    pub fn add_schema(&self, dataset_rid: &str, branch: &str, schema: serde_json::Value) {
        self.schemas
            .lock()
            .unwrap()
            .insert((dataset_rid.to_string(), branch.to_string()), schema);
    }

    pub fn fail_open_for(&self, dataset_rid: &str) {
        *self.fail_open_for.lock().unwrap() = Some(dataset_rid.to_string());
    }
}

#[async_trait]
impl DatasetVersioningClient for MockDatasetClient {
    async fn list_branches(&self, dataset_rid: &str) -> Result<Vec<BranchSnapshot>, ClientError> {
        Ok(self
            .branches
            .lock()
            .unwrap()
            .get(dataset_rid)
            .cloned()
            .unwrap_or_default())
    }

    async fn open_transaction(
        &self,
        dataset_rid: &str,
        _branch: &str,
    ) -> Result<String, ClientError> {
        if let Some(target) = self.fail_open_for.lock().unwrap().as_ref() {
            if target == dataset_rid {
                return Err(ClientError("simulated open failure".into()));
            }
        }
        let mut counter = self.txn_counter.lock().unwrap();
        *counter += 1;
        Ok(format!(
            "ri.foundry.main.transaction.{}-{}",
            dataset_rid, counter
        ))
    }

    async fn view_schema(
        &self,
        dataset_rid: &str,
        branch: &str,
    ) -> Result<serde_json::Value, ClientError> {
        Ok(self
            .schemas
            .lock()
            .unwrap()
            .get(&(dataset_rid.to_string(), branch.to_string()))
            .cloned()
            .unwrap_or_else(|| serde_json::json!({"fields": []})))
    }
}

#[derive(Default)]
pub struct MockJobSpecRepo {
    pub specs: Mutex<HashMap<String, JobSpec>>,
}

impl MockJobSpecRepo {
    pub fn add(&self, spec: JobSpec) {
        for output in spec.output_dataset_rids.clone() {
            self.specs.lock().unwrap().insert(output, spec.clone());
        }
    }
}

#[async_trait]
impl JobSpecRepo for MockJobSpecRepo {
    async fn lookup(
        &self,
        _pipeline_rid: &str,
        output_dataset_rid: &str,
        _build_branch: &str,
        _fallback_chain: &[String],
    ) -> Result<Option<JobSpec>, ClientError> {
        Ok(self.specs.lock().unwrap().get(output_dataset_rid).cloned())
    }
}

// --------------------------------------------------------------------- runner

/// Per-spec recipe for the deterministic [`MockJobRunner`].
#[derive(Debug, Clone)]
pub struct RunnerScript {
    pub outcome: JobOutcome,
    pub sleep: Duration,
}

impl RunnerScript {
    pub fn ok(hash: &str) -> Self {
        Self {
            outcome: JobOutcome::Completed {
                output_content_hash: hash.to_string(),
            },
            sleep: Duration::from_millis(0),
        }
    }
    pub fn fail(reason: &str) -> Self {
        Self {
            outcome: JobOutcome::Failed {
                reason: reason.to_string(),
            },
            sleep: Duration::from_millis(0),
        }
    }
    pub fn with_sleep(mut self, dur: Duration) -> Self {
        self.sleep = dur;
        self
    }
}

#[derive(Default)]
pub struct MockJobRunner {
    pub scripts: Mutex<HashMap<String, RunnerScript>>,
    pub started: Mutex<Vec<String>>,
}

impl MockJobRunner {
    pub fn add(&self, spec_rid: &str, script: RunnerScript) {
        self.scripts
            .lock()
            .unwrap()
            .insert(spec_rid.to_string(), script);
    }
}

#[async_trait]
impl JobRunner for MockJobRunner {
    async fn run(&self, ctx: &JobContext) -> JobOutcome {
        self.started.lock().unwrap().push(ctx.job_spec.rid.clone());
        let script = self.scripts.lock().unwrap().get(&ctx.job_spec.rid).cloned();
        let script =
            script.unwrap_or_else(|| RunnerScript::ok(&format!("default-{}", ctx.job_spec.rid)));
        if !script.sleep.is_zero() {
            tokio::time::sleep(script.sleep).await;
        }
        script.outcome
    }
}

// ----------------------------------------------------------------- output client

#[derive(Default)]
pub struct MockOutputClient {
    pub commits: Mutex<Vec<(String, String)>>,
    pub aborts: Mutex<Vec<(String, String)>>,
    pub fail_commit_for: Mutex<Option<String>>,
}

impl MockOutputClient {
    pub fn fail_commit_for(&self, dataset_rid: &str) {
        *self.fail_commit_for.lock().unwrap() = Some(dataset_rid.to_string());
    }
}

#[async_trait]
impl OutputTransactionClient for MockOutputClient {
    async fn commit(
        &self,
        dataset_rid: &str,
        transaction_rid: &str,
    ) -> Result<(), OutputClientError> {
        if let Some(target) = self.fail_commit_for.lock().unwrap().as_ref() {
            if target == dataset_rid {
                return Err(OutputClientError("simulated commit failure".into()));
            }
        }
        self.commits
            .lock()
            .unwrap()
            .push((dataset_rid.to_string(), transaction_rid.to_string()));
        Ok(())
    }
    async fn abort(
        &self,
        dataset_rid: &str,
        transaction_rid: &str,
    ) -> Result<(), OutputClientError> {
        self.aborts
            .lock()
            .unwrap()
            .push((dataset_rid.to_string(), transaction_rid.to_string()));
        Ok(())
    }
}

pub fn arc_runner(runner: MockJobRunner) -> Arc<dyn JobRunner> {
    Arc::new(runner)
}
pub fn arc_output(client: MockOutputClient) -> Arc<dyn OutputTransactionClient> {
    Arc::new(client)
}

pub fn job_spec(rid: &str, inputs: Vec<&str>, outputs: Vec<&str>) -> JobSpec {
    use pipeline_build_service::domain::build_resolution::InputSpec;
    JobSpec {
        rid: rid.to_string(),
        pipeline_rid: "ri.foundry.main.pipeline.test".to_string(),
        branch_name: "master".to_string(),
        inputs: inputs
            .into_iter()
            .map(|d| InputSpec {
                dataset_rid: d.to_string(),
                fallback_chain: vec!["master".into()],
                view_filter: vec![],
                require_fresh: false,
            })
            .collect(),
        output_dataset_rids: outputs.into_iter().map(String::from).collect(),
        logic_kind: "TRANSFORM".to_string(),
        logic_payload: serde_json::Value::Null,
        content_hash: format!("hash-{rid}"),
    }
}
