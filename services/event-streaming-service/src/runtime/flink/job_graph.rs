//! HTTP proxy for Flink JobManager job-graph endpoint.
//!
//! Backs the REST handler `GET /topologies/:id/job-graph`. Surfaces the
//! Flink-native vertex/edge graph (the same payload the Flink Web UI
//! uses) so the front-end can render it with cytoscape.

use serde_json::Value;

use crate::runtime::flink::FlinkRuntimeConfig;

#[derive(Debug, thiserror::Error)]
pub enum JobGraphError {
    #[error("missing flink_deployment_name on topology")]
    NotDeployed,
    #[error("flink jobmanager returned status {0}")]
    Status(u16),
    #[error("http: {0}")]
    Http(#[from] reqwest::Error),
    #[error("invalid response: {0}")]
    Body(String),
}

/// Fetch and normalise the job graph. Output shape:
///
/// ```json
/// {
///   "job_id": "...",
///   "vertices": [{ "id": "...", "name": "...", "parallelism": 4 }],
///   "edges":    [{ "source": "...", "target": "..." }],
///   "raw":      { ... }   // verbatim Flink response for debugging
/// }
/// ```
pub async fn fetch_job_graph(
    cfg: &FlinkRuntimeConfig,
    deployment: &str,
    namespace: &str,
    job_id: Option<&str>,
) -> Result<Value, JobGraphError> {
    let client = reqwest::Client::builder()
        .timeout(std::time::Duration::from_secs(10))
        .build()?;
    let base = cfg.jobmanager_url(deployment, namespace);
    let resolved_job = match job_id {
        Some(id) => id.to_string(),
        None => discover(&client, &base).await?,
    };
    let url = format!("{base}/jobs/{resolved_job}");
    let resp = client.get(&url).send().await?;
    if !resp.status().is_success() {
        return Err(JobGraphError::Status(resp.status().as_u16()));
    }
    let raw: Value = resp.json().await?;
    Ok(normalise(raw, &resolved_job))
}

async fn discover(client: &reqwest::Client, base: &str) -> Result<String, JobGraphError> {
    let body: Value = client
        .get(&format!("{base}/jobs"))
        .send()
        .await?
        .error_for_status()
        .map_err(|e| JobGraphError::Status(e.status().map(|s| s.as_u16()).unwrap_or(0)))?
        .json()
        .await?;
    let jobs = body
        .get("jobs")
        .and_then(|v| v.as_array())
        .ok_or_else(|| JobGraphError::Body("no 'jobs' array".into()))?;
    let chosen = jobs
        .iter()
        .find(|j| j.get("status").and_then(|s| s.as_str()) == Some("RUNNING"))
        .or_else(|| jobs.first())
        .and_then(|j| j.get("id").and_then(|s| s.as_str()))
        .ok_or_else(|| JobGraphError::Body("no jobs reported".into()))?
        .to_string();
    Ok(chosen)
}

/// Pure helper: pluck the cytoscape-friendly subset out of a Flink
/// `/jobs/{id}` response.
pub fn normalise(raw: Value, job_id: &str) -> Value {
    let vertices: Vec<Value> = raw
        .get("vertices")
        .and_then(|v| v.as_array())
        .map(|arr| {
            arr.iter()
                .map(|v| {
                    serde_json::json!({
                        "id": v.get("id").cloned().unwrap_or(Value::Null),
                        "name": v.get("name").cloned().unwrap_or(Value::Null),
                        "parallelism": v.get("parallelism").cloned().unwrap_or(Value::Null),
                        "status": v.get("status").cloned().unwrap_or(Value::Null),
                    })
                })
                .collect()
        })
        .unwrap_or_default();
    let edges: Vec<Value> = raw
        .get("plan")
        .and_then(|p| p.get("nodes"))
        .and_then(|n| n.as_array())
        .map(|nodes| {
            let mut out = Vec::new();
            for node in nodes {
                if let (Some(target), Some(inputs)) = (
                    node.get("id").and_then(|v| v.as_str()),
                    node.get("inputs").and_then(|v| v.as_array()),
                ) {
                    for input in inputs {
                        if let Some(src) = input.get("id").and_then(|v| v.as_str()) {
                            out.push(serde_json::json!({
                                "source": src,
                                "target": target,
                            }));
                        }
                    }
                }
            }
            out
        })
        .unwrap_or_default();
    serde_json::json!({
        "job_id": job_id,
        "vertices": vertices,
        "edges": edges,
        "raw": raw,
    })
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn normalise_extracts_vertices_and_edges_from_flink_payload() {
        let payload = json!({
            "vertices": [
                {"id": "v1", "name": "Source", "parallelism": 2, "status": "RUNNING"},
                {"id": "v2", "name": "Sink",   "parallelism": 4, "status": "RUNNING"},
            ],
            "plan": {
                "nodes": [
                    {"id": "v2", "inputs": [{"id": "v1"}]}
                ]
            }
        });
        let out = normalise(payload, "job-x");
        assert_eq!(out["job_id"], "job-x");
        assert_eq!(out["vertices"].as_array().unwrap().len(), 2);
        assert_eq!(out["edges"][0]["source"], "v1");
        assert_eq!(out["edges"][0]["target"], "v2");
    }
}
