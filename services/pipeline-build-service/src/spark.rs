//! FASE 3 / Tarea 3.4 — submit `SparkApplication` CRs to the Spark
//! Operator on behalf of `pipeline-build-service`.
//!
//! This module replaces the Temporal `PipelineRun` workflow + the
//! `ExecutePipeline` activity pair documented in
//! [`docs/architecture/refactor/pipeline-worker-inventory.md`]. The CR
//! template lives in
//! [`infra/helm/infra/spark-jobs/templates/_pipeline-run-template.yaml`]
//! (Tarea 3.2); the runner image it points at is built by Tarea 3.3.
//!
//! Surface:
//! * [`render_manifest`] — pure function. Substitutes the `${var}`
//!   placeholders in the embedded template with values from a
//!   [`PipelineRunInput`] and returns the parsed JSON object. Exposed
//!   so the unit tests can pin the rendering rules without going
//!   through `kube`.
//! * [`submit_pipeline_run`] — POSTs the rendered manifest to the
//!   Kubernetes API as a `sparkoperator.k8s.io/v1beta2/SparkApplication`
//!   `DynamicObject`. Returns the `metadata.name` actually accepted by
//!   the API server.
//! * [`get_pipeline_run_status`] — reads `.status.applicationState.state`
//!   on an existing CR and maps it to [`SparkRunStatus`].
//!
//! The Spark Operator GVK and the `${var}` substitution syntax are
//! deliberately decoupled from Helm — see
//! `infra/helm/infra/spark-jobs/README-pipeline-run.md` for the
//! rationale (we need a substitution syntax that survives `helm
//! install` untouched and is renderable from Rust without Tera /
//! Handlebars).

use std::collections::BTreeMap;

use kube::Client;
use kube::api::{Api, DynamicObject, GroupVersionKind, PostParams};
use kube::core::ApiResource;
use serde::{Deserialize, Serialize};
use serde_json::Value as JsonValue;
use thiserror::Error;

/// Embedded copy of the Tarea 3.2 SparkApplication template. The
/// build pulls it in via `include_str!` so the binary is fully
/// self-contained — no runtime file lookups, no chart-version drift
/// between the Rust service and the Spark operator chart.
pub const PIPELINE_RUN_TEMPLATE: &str =
    include_str!("../../../infra/helm/infra/spark-jobs/templates/_pipeline-run-template.yaml");

/// `apiVersion` / `kind` of the SparkApplication CRD owned by the
/// Spark Operator (chart `infra/helm/infra/spark-operator/`).
pub const SPARK_GROUP: &str = "sparkoperator.k8s.io";
pub const SPARK_VERSION: &str = "v1beta2";
pub const SPARK_KIND: &str = "SparkApplication";
pub const SPARK_PLURAL: &str = "sparkapplications";

/// Maximum length of `pipeline-run-${pipeline_id}-${run_id}` — the
/// Spark Operator appends a per-Pod suffix and the resulting driver
/// Pod name is bounded by Kubernetes' 63-char DNS-1123 label limit.
/// See `infra/helm/infra/spark-jobs/README-pipeline-run.md`.
pub const MAX_SPARK_APP_NAME_LEN: usize = 50;

/// Inputs required to render the SparkApplication CR. Every field
/// maps 1:1 to a `${...}` placeholder in the embedded template.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct PipelineRunInput {
    /// Stable identifier of the pipeline this run belongs to. Used
    /// verbatim as the `${pipeline_id}` placeholder.
    pub pipeline_id: String,
    /// Per-run identifier (ULID, short UUID, …). Used verbatim as
    /// `${run_id}`.
    pub run_id: String,
    /// Kubernetes namespace the CR is created in. Must be the
    /// namespace the Spark Operator watches.
    pub namespace: String,
    /// `Scala` for `sql`/`spark` transform nodes, `Python` for
    /// `pyspark` nodes. See the engine dispatch table in the
    /// Tarea 3.2 README.
    pub application_type: SparkApplicationType,
    /// Container image built by Tarea 3.3.
    pub pipeline_runner_image: String,
    /// Iceberg/Lakekeeper RID of the input table.
    pub input_dataset_rid: String,
    /// Iceberg/Lakekeeper RID of the output table.
    pub output_dataset_rid: String,
    /// Optional Spark resource overrides; defaults match the README.
    #[serde(default)]
    pub resources: SparkResourceOverrides,
}

/// Lightweight enum for `${spark_application_type}`.
#[derive(Debug, Clone, Copy, Serialize, Deserialize, PartialEq, Eq)]
#[serde(rename_all = "PascalCase")]
pub enum SparkApplicationType {
    Scala,
    Python,
}

impl SparkApplicationType {
    fn as_str(self) -> &'static str {
        match self {
            Self::Scala => "Scala",
            Self::Python => "Python",
        }
    }

    /// Default `mainClass` / `mainApplicationFile` pair baked into
    /// the runner image (see `services/pipeline-runner/Dockerfile`).
    fn defaults(self) -> (&'static str, &'static str) {
        match self {
            Self::Scala => (
                "com.openfoundry.pipeline.PipelineRunner",
                "local:///opt/spark/jars/pipeline-runner.jar",
            ),
            // The Python runner is provisioned for parity with
            // pyspark-typed transform nodes; the actual entrypoint
            // lands together with the Python flavour of Tarea 3.3.
            Self::Python => (
                "",
                "local:///opt/spark/work-dir/pipeline_runner.py",
            ),
        }
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SparkResourceOverrides {
    pub driver_cores: u32,
    pub driver_memory: String,
    pub executor_cores: u32,
    pub executor_instances: u32,
    pub executor_memory: String,
}

impl Default for SparkResourceOverrides {
    fn default() -> Self {
        // Matches the defaults documented in
        // `infra/helm/infra/spark-jobs/README-pipeline-run.md`.
        Self {
            driver_cores: 1,
            driver_memory: "1g".to_string(),
            executor_cores: 1,
            executor_instances: 2,
            executor_memory: "2g".to_string(),
        }
    }
}

/// State the handler exposes back to the caller of
/// `GET /api/v1/pipeline/builds/{run_id}/status`. Mirrors the
/// `pipeline_run_submissions.status` CHECK constraint in
/// `migrations/20260504000080_pipeline_run_submissions.sql`.
#[derive(Debug, Clone, Copy, Serialize, Deserialize, PartialEq, Eq)]
#[serde(rename_all = "UPPERCASE")]
pub enum SparkRunStatus {
    Submitted,
    Running,
    Succeeded,
    Failed,
    Unknown,
}

impl SparkRunStatus {
    pub fn as_str(self) -> &'static str {
        match self {
            Self::Submitted => "SUBMITTED",
            Self::Running => "RUNNING",
            Self::Succeeded => "SUCCEEDED",
            Self::Failed => "FAILED",
            Self::Unknown => "UNKNOWN",
        }
    }
}

#[derive(Debug, Clone, Serialize)]
pub struct SparkRunStatusReport {
    pub status: SparkRunStatus,
    pub error_message: Option<String>,
}

#[derive(Debug, Error)]
pub enum SparkSubmitError {
    #[error("invalid pipeline run input: {0}")]
    InvalidInput(String),
    #[error("template rendering failed: {0}")]
    Render(String),
    #[error("kubernetes api call failed: {0}")]
    Kube(#[from] kube::Error),
}

// ---------------------------------------------------------------------------
// Pure rendering — exposed for unit tests in `tests/spark_render.rs`.
// ---------------------------------------------------------------------------

/// Render the Tarea 3.2 template into a `serde_json::Value` ready to
/// be sent through `kube::Api::create`. Performs `${var}` substitution
/// followed by a YAML→JSON parse.
pub fn render_manifest(input: &PipelineRunInput) -> Result<JsonValue, SparkSubmitError> {
    validate_input(input)?;

    let app_name = spark_app_name(&input.pipeline_id, &input.run_id)?;
    let (default_main_class, default_main_file) = input.application_type.defaults();

    let substitutions: BTreeMap<&str, String> = BTreeMap::from([
        // Composite name is precomputed so the same value is used in
        // metadata.name and in any future label that needs it.
        ("pipeline_id", input.pipeline_id.clone()),
        ("run_id", input.run_id.clone()),
        ("namespace", input.namespace.clone()),
        (
            "spark_application_type",
            input.application_type.as_str().to_string(),
        ),
        (
            "pipeline_runner_image",
            input.pipeline_runner_image.clone(),
        ),
        ("main_class", default_main_class.to_string()),
        ("main_application_file", default_main_file.to_string()),
        ("input_dataset_rid", input.input_dataset_rid.clone()),
        ("output_dataset_rid", input.output_dataset_rid.clone()),
        ("driver_cores", input.resources.driver_cores.to_string()),
        ("driver_memory", input.resources.driver_memory.clone()),
        (
            "executor_cores",
            input.resources.executor_cores.to_string(),
        ),
        (
            "executor_instances",
            input.resources.executor_instances.to_string(),
        ),
        (
            "executor_memory",
            input.resources.executor_memory.clone(),
        ),
    ]);

    let rendered = substitute(PIPELINE_RUN_TEMPLATE, &substitutions);
    if let Some(missing) = first_unsubstituted_placeholder(&rendered) {
        return Err(SparkSubmitError::Render(format!(
            "unresolved placeholder ${{{missing}}} in pipeline-run template"
        )));
    }

    let yaml: serde_yaml::Value = serde_yaml::from_str(&rendered)
        .map_err(|e| SparkSubmitError::Render(format!("YAML parse: {e}")))?;
    let mut json: JsonValue = serde_json::to_value(&yaml)
        .map_err(|e| SparkSubmitError::Render(format!("YAML→JSON: {e}")))?;

    // Force metadata.name even though the template already uses it —
    // this guards against a future template edit that could drop the
    // composite expression. We *always* know the canonical name here.
    if let Some(meta) = json
        .get_mut("metadata")
        .and_then(|m| m.as_object_mut())
    {
        meta.insert("name".into(), JsonValue::String(app_name));
    }

    Ok(json)
}

/// Compose the canonical `pipeline-run-<pipeline>-<run>` name and
/// fail loudly if it would exceed the 50-char budget the template
/// README enforces.
pub fn spark_app_name(pipeline_id: &str, run_id: &str) -> Result<String, SparkSubmitError> {
    let name = format!("pipeline-run-{pipeline_id}-{run_id}");
    if name.len() > MAX_SPARK_APP_NAME_LEN {
        return Err(SparkSubmitError::InvalidInput(format!(
            "computed SparkApplication name {:?} ({} chars) exceeds the {}-char limit; \
             truncate pipeline_id / run_id before submission",
            name,
            name.len(),
            MAX_SPARK_APP_NAME_LEN
        )));
    }
    Ok(name)
}

fn validate_input(input: &PipelineRunInput) -> Result<(), SparkSubmitError> {
    for (label, value) in [
        ("pipeline_id", &input.pipeline_id),
        ("run_id", &input.run_id),
        ("namespace", &input.namespace),
        ("pipeline_runner_image", &input.pipeline_runner_image),
        ("input_dataset_rid", &input.input_dataset_rid),
        ("output_dataset_rid", &input.output_dataset_rid),
    ] {
        if value.trim().is_empty() {
            return Err(SparkSubmitError::InvalidInput(format!(
                "{label} must not be empty"
            )));
        }
    }
    Ok(())
}

/// Substitute `${var}` occurrences in `template`. Unknown placeholders
/// are left untouched so [`first_unsubstituted_placeholder`] can flag
/// them.
fn substitute(template: &str, vars: &BTreeMap<&str, String>) -> String {
    let mut out = template.to_string();
    for (key, value) in vars {
        out = out.replace(&format!("${{{key}}}"), value);
    }
    out
}

/// Returns the name of the first `${var}` left in `s`, if any. Used
/// to guarantee no placeholder leaks into the manifest the API server
/// sees (the Spark Operator would otherwise reject the CR with an
/// unhelpful YAML parse error).
fn first_unsubstituted_placeholder(s: &str) -> Option<String> {
    let bytes = s.as_bytes();
    let mut i = 0;
    while i + 1 < bytes.len() {
        if bytes[i] == b'$' && bytes[i + 1] == b'{' {
            let start = i + 2;
            if let Some(end_off) = s[start..].find('}') {
                let name = &s[start..start + end_off];
                // Skip Helm `${ }` pieces that contain whitespace —
                // the template only uses simple `${ident}` names.
                if !name.is_empty() && name.chars().all(|c| c.is_ascii_alphanumeric() || c == '_')
                {
                    return Some(name.to_string());
                }
                i = start + end_off + 1;
                continue;
            }
        }
        i += 1;
    }
    None
}

// ---------------------------------------------------------------------------
// Kubernetes client surface.
// ---------------------------------------------------------------------------

/// Build the dynamic `Api<DynamicObject>` handle for the Spark
/// Operator CRD scoped to `namespace`. Exposed so tests can construct
/// the same handle against a `tower_test::mock`-backed `kube::Client`.
pub fn spark_api(client: Client, namespace: &str) -> Api<DynamicObject> {
    let gvk = GroupVersionKind::gvk(SPARK_GROUP, SPARK_VERSION, SPARK_KIND);
    let ar = ApiResource::from_gvk_with_plural(&gvk, SPARK_PLURAL);
    Api::namespaced_with(client, namespace, &ar)
}

/// Render + POST the SparkApplication for `input`. Returns the
/// `metadata.name` returned by the API server (which is what the
/// caller stores in `pipeline_run_submissions.spark_app_name`).
pub async fn submit_pipeline_run(
    client: Client,
    input: &PipelineRunInput,
) -> Result<String, SparkSubmitError> {
    let manifest = render_manifest(input)?;
    let obj: DynamicObject = serde_json::from_value(manifest)
        .map_err(|e| SparkSubmitError::Render(format!("manifest→DynamicObject: {e}")))?;
    let api = spark_api(client, &input.namespace);
    let created = api.create(&PostParams::default(), &obj).await?;
    Ok(created
        .metadata
        .name
        .unwrap_or_else(|| spark_app_name(&input.pipeline_id, &input.run_id).unwrap_or_default()))
}

/// Fetch the SparkApplication CR named `name` in `namespace` and map
/// `.status.applicationState.state` to a [`SparkRunStatusReport`].
/// Returns `Ok(None)` if the CR no longer exists (typically because
/// the operator's `timeToLiveSeconds` reaped it).
pub async fn get_pipeline_run_status(
    client: Client,
    namespace: &str,
    name: &str,
) -> Result<Option<SparkRunStatusReport>, SparkSubmitError> {
    let api = spark_api(client, namespace);
    let obj = match api.get_opt(name).await? {
        Some(obj) => obj,
        None => return Ok(None),
    };
    Ok(Some(parse_status(&obj.data)))
}

fn parse_status(body: &JsonValue) -> SparkRunStatusReport {
    let app_state = body
        .get("status")
        .and_then(|s| s.get("applicationState"))
        .cloned()
        .unwrap_or(JsonValue::Null);

    let state = app_state
        .get("state")
        .and_then(|v| v.as_str())
        .unwrap_or("");
    let error_message = app_state
        .get("errorMessage")
        .and_then(|v| v.as_str())
        .filter(|s| !s.is_empty())
        .map(|s| s.to_string());

    // Spark Operator state vocabulary:
    // <https://github.com/kubeflow/spark-operator/blob/master/docs/api-docs.md#sparkapplicationstate>
    let status = match state {
        // The operator publishes both empty (just-submitted) and
        // explicit "SUBMITTED" states; both map to the same row.
        "" | "SUBMITTED" | "PENDING_SUBMISSION" | "PENDING_RERUN" => SparkRunStatus::Submitted,
        "RUNNING" | "INVALIDATING" | "SUCCEEDING" | "FAILING" => SparkRunStatus::Running,
        "COMPLETED" => SparkRunStatus::Succeeded,
        "FAILED" | "FAILED_SUBMISSION" => SparkRunStatus::Failed,
        "UNKNOWN" => SparkRunStatus::Unknown,
        _ => SparkRunStatus::Unknown,
    };

    SparkRunStatusReport {
        status,
        error_message,
    }
}

// ---------------------------------------------------------------------------
// Unit tests — pure rendering only. The kube round-trip lives in
// `tests/spark_submit_kube_stub.rs` so it can use `tower_test::mock`
// without polluting the binary's dep graph.
// ---------------------------------------------------------------------------

#[cfg(test)]
mod tests {
    use super::*;

    fn sample_input() -> PipelineRunInput {
        PipelineRunInput {
            pipeline_id: "p-7c1a".into(),
            run_id: "r-01HF9P".into(),
            namespace: "openfoundry-spark".into(),
            application_type: SparkApplicationType::Scala,
            pipeline_runner_image: "ghcr.io/unnamedlab/pipeline-runner:0.1.0".into(),
            input_dataset_rid: "ri.dataset.main.4abc".into(),
            output_dataset_rid: "ri.dataset.main.9def".into(),
            resources: SparkResourceOverrides::default(),
        }
    }

    #[test]
    fn renders_with_canonical_name_and_arguments() {
        let json = render_manifest(&sample_input()).expect("render");
        assert_eq!(
            json["apiVersion"],
            format!("{SPARK_GROUP}/{SPARK_VERSION}")
        );
        assert_eq!(json["kind"], SPARK_KIND);
        assert_eq!(
            json["metadata"]["name"],
            "pipeline-run-p-7c1a-r-01HF9P"
        );
        assert_eq!(json["metadata"]["namespace"], "openfoundry-spark");
        assert_eq!(json["spec"]["type"], "Scala");
        assert_eq!(
            json["spec"]["mainClass"],
            "com.openfoundry.pipeline.PipelineRunner"
        );
        assert_eq!(
            json["spec"]["mainApplicationFile"],
            "local:///opt/spark/jars/pipeline-runner.jar"
        );
        let args = json["spec"]["arguments"]
            .as_array()
            .expect("arguments must be a list");
        assert!(args.iter().any(|a| a == "ri.dataset.main.4abc"));
        assert!(args.iter().any(|a| a == "ri.dataset.main.9def"));
    }

    #[test]
    fn rejects_input_with_empty_required_field() {
        let mut bad = sample_input();
        bad.pipeline_id = "  ".into();
        let err = render_manifest(&bad).expect_err("must reject blank pipeline_id");
        assert!(matches!(err, SparkSubmitError::InvalidInput(_)));
    }

    #[test]
    fn rejects_input_whose_composite_name_exceeds_50_chars() {
        let mut bad = sample_input();
        // pipeline-run- (13) + pipeline_id (40) + - (1) + run_id (1) = 55
        bad.pipeline_id = "x".repeat(40);
        bad.run_id = "y".into();
        let err = render_manifest(&bad).expect_err("must reject overlong name");
        match err {
            SparkSubmitError::InvalidInput(msg) => assert!(msg.contains("50-char limit"), "{msg}"),
            other => panic!("unexpected error variant: {other:?}"),
        }
    }

    #[test]
    fn parse_status_maps_known_states() {
        let body = serde_json::json!({
            "status": {"applicationState": {"state": "RUNNING"}}
        });
        assert_eq!(parse_status(&body).status, SparkRunStatus::Running);

        let body = serde_json::json!({
            "status": {"applicationState": {"state": "COMPLETED"}}
        });
        assert_eq!(parse_status(&body).status, SparkRunStatus::Succeeded);

        let body = serde_json::json!({
            "status": {"applicationState": {
                "state": "FAILED",
                "errorMessage": "OOM in driver"
            }}
        });
        let report = parse_status(&body);
        assert_eq!(report.status, SparkRunStatus::Failed);
        assert_eq!(report.error_message.as_deref(), Some("OOM in driver"));

        // Missing status block — the operator hasn't written one yet.
        assert_eq!(
            parse_status(&serde_json::json!({})).status,
            SparkRunStatus::Submitted
        );
    }

    #[test]
    fn first_unsubstituted_placeholder_finds_simple_idents_only() {
        assert_eq!(
            first_unsubstituted_placeholder("hello ${world} foo").as_deref(),
            Some("world")
        );
        // Helm `{{ ... }}` style is not our concern.
        assert!(first_unsubstituted_placeholder("hello {{ .Foo }}").is_none());
        // Strings that look like `${ ... }` but contain spaces are not
        // ours either (defensive — the template doesn't use them).
        assert!(first_unsubstituted_placeholder("hello ${foo bar}").is_none());
    }
}
