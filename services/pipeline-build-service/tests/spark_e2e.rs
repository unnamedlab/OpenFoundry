//! FASE 3 / Tarea 3.7 — End-to-end smoke test for the
//! `API call → SparkApplication → Iceberg write` round-trip.
//!
//! This test is **deliberately not** a testcontainers test: the Spark
//! Operator is a `CustomResourceDefinition` controller and only runs
//! against a real Kubernetes API server. It is meant to execute
//! against a `lima` cluster (local dev) or the staging cluster
//! provisioned by `helmfile -e dev apply` (Phase 9 of the migration
//! plan), so it is gated by:
//!
//! 1. The `spark-e2e` Cargo feature (enables the Iceberg client deps),
//! 2. The `#[ignore]` attribute (default `cargo test` skips it).
//!
//! How to run:
//!
//! ```text
//! cargo test -p pipeline-build-service --features spark-e2e \
//!   --test spark_e2e -- --ignored --nocapture
//! ```
//!
//! Required environment (the test panics with a clear message if any
//! is missing — there is no implicit default that would silently hit
//! a misconfigured cluster):
//!
//! | Variable                              | Example                                        |
//! |---------------------------------------|------------------------------------------------|
//! | `OF_PIPELINE_BUILD_SERVICE_URL`       | `http://pipeline-build-service.openfoundry:8080` |
//! | `OF_PIPELINE_INPUT_DATASET_RID`       | `ri.dataset.main.fixture-10rows`               |
//! | `OF_PIPELINE_OUTPUT_DATASET_RID`     | `ri.dataset.main.spark-e2e-out`                |
//! | `OF_LAKEKEEPER_CATALOG_URL`           | `http://lakekeeper.openfoundry:8181/catalog`   |
//! | `OF_PIPELINE_OUTPUT_NAMESPACE`        | `main.spark_e2e` (dot-separated namespace path)|
//! | `OF_PIPELINE_OUTPUT_TABLE`            | `out`                                          |
//!
//! Optional:
//!
//! | Variable                              | Default                  | Notes                                       |
//! |---------------------------------------|--------------------------|---------------------------------------------|
//! | `OF_LAKEKEEPER_WAREHOUSE`             | _(unset)_                | Forwarded as `warehouse` REST property.     |
//! | `OF_PIPELINE_ID`                      | `spark-e2e`              | `pipeline_id` field of the submission.      |
//! | `OF_PIPELINE_E2E_TIMEOUT_SECS`        | `600`                    | Total timeout from POST to `SUCCEEDED`.     |
//! | `OF_PIPELINE_E2E_POLL_INTERVAL_SECS`  | `5`                      | Status poll cadence.                        |
//! | `OF_PIPELINE_E2E_MIN_ROWS`            | `10`                     | Minimum row count expected in the output.   |
//!
//! Failure model: per the task spec, Spark exec time is variable, so
//! the timeout is generous (10 minutes by default) and the per-state
//! assertions are explicit (`SUCCEEDED` ⇒ pass, `FAILED` ⇒ fail
//! immediately with the operator's `errorMessage`, anything else ⇒
//! keep polling until the timeout elapses).

#![cfg(feature = "spark-e2e")]

use std::time::{Duration, Instant};

use serde_json::{Value as JsonValue, json};
use storage_abstraction::iceberg::IcebergTable;

const ENV_SERVICE_URL: &str = "OF_PIPELINE_BUILD_SERVICE_URL";
const ENV_INPUT_RID: &str = "OF_PIPELINE_INPUT_DATASET_RID";
const ENV_OUTPUT_RID: &str = "OF_PIPELINE_OUTPUT_DATASET_RID";
const ENV_CATALOG_URL: &str = "OF_LAKEKEEPER_CATALOG_URL";
const ENV_OUTPUT_NAMESPACE: &str = "OF_PIPELINE_OUTPUT_NAMESPACE";
const ENV_OUTPUT_TABLE: &str = "OF_PIPELINE_OUTPUT_TABLE";

const ENV_WAREHOUSE: &str = "OF_LAKEKEEPER_WAREHOUSE";
const ENV_PIPELINE_ID: &str = "OF_PIPELINE_ID";
const ENV_TIMEOUT_SECS: &str = "OF_PIPELINE_E2E_TIMEOUT_SECS";
const ENV_POLL_INTERVAL_SECS: &str = "OF_PIPELINE_E2E_POLL_INTERVAL_SECS";
const ENV_MIN_ROWS: &str = "OF_PIPELINE_E2E_MIN_ROWS";

const DEFAULT_PIPELINE_ID: &str = "spark-e2e";
const DEFAULT_TIMEOUT_SECS: u64 = 600;
const DEFAULT_POLL_INTERVAL_SECS: u64 = 5;
const DEFAULT_MIN_ROWS: usize = 10;

/// Read a required env var, panicking with a self-describing message
/// when missing. We deliberately panic (rather than silently skip)
/// because the test is already `#[ignore]`d — anyone running it has
/// asked for the staging contract to be exercised.
fn required_env(name: &str) -> String {
    std::env::var(name).unwrap_or_else(|_| {
        panic!(
            "spark_e2e: required env var `{name}` is not set; \
             see the module-level docs for the full list"
        )
    })
}

fn optional_env(name: &str) -> Option<String> {
    std::env::var(name).ok().filter(|s| !s.is_empty())
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires a real Kubernetes cluster with Spark Operator + Lakekeeper (CI staging only)"]
async fn pipeline_run_writes_iceberg_output_end_to_end() {
    // ---- 1. Resolve cluster + fixture configuration -----------------
    let service_url = required_env(ENV_SERVICE_URL)
        .trim_end_matches('/')
        .to_string();
    let input_rid = required_env(ENV_INPUT_RID);
    let output_rid = required_env(ENV_OUTPUT_RID);
    let catalog_url = required_env(ENV_CATALOG_URL);
    let output_namespace = required_env(ENV_OUTPUT_NAMESPACE);
    let output_table = required_env(ENV_OUTPUT_TABLE);

    let warehouse = optional_env(ENV_WAREHOUSE);
    let pipeline_id =
        optional_env(ENV_PIPELINE_ID).unwrap_or_else(|| DEFAULT_PIPELINE_ID.to_string());
    let timeout = Duration::from_secs(parse_or_default(ENV_TIMEOUT_SECS, DEFAULT_TIMEOUT_SECS));
    let poll_interval = Duration::from_secs(parse_or_default(
        ENV_POLL_INTERVAL_SECS,
        DEFAULT_POLL_INTERVAL_SECS,
    ));
    let min_rows: usize = parse_or_default(ENV_MIN_ROWS, DEFAULT_MIN_ROWS as u64) as usize;

    // ---- 2. POST /api/v1/pipeline/builds/run -------------------------
    let http = reqwest::Client::builder()
        .timeout(Duration::from_secs(30))
        .build()
        .expect("build reqwest client");

    let submit_url = format!("{service_url}/api/v1/pipeline/builds/run");
    let submit_body = json!({
        "pipeline_id": pipeline_id,
        "input_dataset_rid": input_rid,
        "output_dataset_rid": output_rid,
    });

    let submit_resp = http
        .post(&submit_url)
        .json(&submit_body)
        .send()
        .await
        .unwrap_or_else(|e| panic!("POST {submit_url} failed: {e}"));

    let submit_status = submit_resp.status();
    let submit_text = submit_resp
        .text()
        .await
        .unwrap_or_else(|e| format!("<failed to read body: {e}>"));
    assert!(
        submit_status.is_success(),
        "POST /api/v1/pipeline/builds/run returned {submit_status}: {submit_text}"
    );

    let submit_json: JsonValue = serde_json::from_str(&submit_text).unwrap_or_else(|e| {
        panic!("submit response was not JSON ({e}): {submit_text}")
    });
    let pipeline_run_id = submit_json
        .get("pipeline_run_id")
        .and_then(|v| v.as_str())
        .unwrap_or_else(|| panic!("submit response missing `pipeline_run_id`: {submit_json}"))
        .to_string();
    let initial_status = submit_json
        .get("status")
        .and_then(|v| v.as_str())
        .unwrap_or("");
    assert_eq!(
        initial_status, "SUBMITTED",
        "expected initial status `SUBMITTED`, got `{initial_status}` (full body: {submit_json})"
    );

    // ---- 3. Poll status until SUCCEEDED, FAILED, or timeout ----------
    let status_url =
        format!("{service_url}/api/v1/pipeline/builds/{pipeline_run_id}/status");
    let started_at = Instant::now();
    let final_status: String = loop {
        let elapsed = started_at.elapsed();
        if elapsed >= timeout {
            panic!(
                "spark_e2e: pipeline_run_id={pipeline_run_id} did not reach SUCCEEDED within \
                 {}s (last status URL: {status_url})",
                timeout.as_secs()
            );
        }

        let resp = match http.get(&status_url).send().await {
            Ok(r) => r,
            Err(e) => {
                eprintln!(
                    "spark_e2e: GET {status_url} transport error after {}s: {e} — retrying",
                    elapsed.as_secs()
                );
                tokio::time::sleep(poll_interval).await;
                continue;
            }
        };

        let resp_status = resp.status();
        let resp_text = resp
            .text()
            .await
            .unwrap_or_else(|e| format!("<failed to read body: {e}>"));
        if !resp_status.is_success() {
            panic!(
                "GET {status_url} returned {resp_status} after {}s: {resp_text}",
                elapsed.as_secs()
            );
        }

        let body: JsonValue = serde_json::from_str(&resp_text).unwrap_or_else(|e| {
            panic!("status response was not JSON ({e}): {resp_text}")
        });
        let state = body
            .get("status")
            .and_then(|v| v.as_str())
            .unwrap_or("")
            .to_string();
        let error_message = body
            .get("error_message")
            .and_then(|v| v.as_str())
            .unwrap_or("");

        match state.as_str() {
            "SUCCEEDED" => break state,
            "FAILED" => panic!(
                "SparkApplication for pipeline_run_id={pipeline_run_id} reached FAILED after \
                 {}s; errorMessage={error_message:?}; full body: {body}",
                elapsed.as_secs()
            ),
            // SUBMITTED / RUNNING / UNKNOWN — keep polling. UNKNOWN
            // can be transient while the operator is still writing the
            // first status block, so we don't fail immediately.
            other => {
                eprintln!(
                    "spark_e2e: status={other} after {}s (run={pipeline_run_id}) — sleeping {}s",
                    elapsed.as_secs(),
                    poll_interval.as_secs()
                );
            }
        }

        tokio::time::sleep(poll_interval).await;
    };
    assert_eq!(final_status, "SUCCEEDED");

    // ---- 4. Read the Iceberg output table and validate row count -----
    let namespace_levels: Vec<&str> = output_namespace
        .split('.')
        .filter(|s| !s.is_empty())
        .collect();
    assert!(
        !namespace_levels.is_empty(),
        "{ENV_OUTPUT_NAMESPACE} must contain at least one dot-separated level, got `{output_namespace}`"
    );

    let table = match warehouse.as_deref() {
        Some(wh) => IcebergTable::load_table_with_warehouse(
            &catalog_url,
            wh,
            &namespace_levels,
            &output_table,
        )
        .await
        .unwrap_or_else(|e| {
            panic!(
                "load Iceberg table {namespace_levels:?}.{output_table} from \
                 {catalog_url} (warehouse={wh}): {e}"
            )
        }),
        None => IcebergTable::load_table(&catalog_url, &namespace_levels, &output_table)
            .await
            .unwrap_or_else(|e| {
                panic!(
                    "load Iceberg table {namespace_levels:?}.{output_table} from \
                     {catalog_url}: {e}"
                )
            }),
    };

    let batches = table
        .scan_to_record_batches(None, None)
        .await
        .unwrap_or_else(|e| panic!("scan output Iceberg table: {e}"));
    let row_count: usize = batches.iter().map(|b| b.num_rows()).sum();
    assert!(
        row_count >= min_rows,
        "spark_e2e: output Iceberg table has {row_count} rows, expected ≥ {min_rows} \
         (namespace={namespace_levels:?}, table={output_table})"
    );
}

fn parse_or_default(name: &str, default: u64) -> u64 {
    match std::env::var(name) {
        Ok(s) if !s.is_empty() => s.parse::<u64>().unwrap_or_else(|e| {
            panic!("spark_e2e: env var `{name}` must be a positive integer ({e}): `{s}`")
        }),
        _ => default,
    }
}
