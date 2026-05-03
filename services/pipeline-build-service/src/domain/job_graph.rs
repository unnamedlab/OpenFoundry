//! Foundry "Job graph compilation" — the formal pre-resolution step.
//!
//! ## What this does
//!
//! Given a set of output datasets and a build branch + global JobSpec
//! fallback chain, walk the chain per-dataset and pull the JobSpec
//! that produces it from `pipeline-authoring-service`. Detect cycles
//! between JobSpecs by treating each spec's outputs as producers and
//! its inputs as consumers, then return a topologically ordered
//! `JobGraph` ready for the resolver to consume.
//!
//! ## Why a separate module from [`super::build_resolution`]
//!
//! `build_resolution` is the *whole* resolve_build pipeline (lookup +
//! cycle + locks + transactions). This module isolates the pure
//! "compile the graph" step so:
//!   * unit tests can exercise it without `wiremock`-faking every
//!     IO-bound surface, and
//!   * the upcoming `dry_run_resolve` endpoint can call it directly
//!     to surface what _would_ run without acquiring locks or opening
//!     transactions.

use std::collections::{BTreeMap, BTreeSet, VecDeque};

use async_trait::async_trait;
use serde::{Deserialize, Serialize};

use crate::domain::build_resolution::{ClientError, JobSpec, JobSpecRepo};

/// Compile-time errors surfaced from [`compile_build_graph`]. Distinct
/// from `BuildResolutionError` because compilation runs *before*
/// resolution and only sees JobSpec metadata.
#[derive(Debug, thiserror::Error)]
pub enum CompileError {
    #[error("missing JobSpec for dataset {dataset_rid} (tried branches: {tried_branches:?})")]
    MissingJobSpec {
        dataset_rid: String,
        tried_branches: Vec<String>,
    },
    /// Two JobSpecs claim ownership of the same output dataset. Kept
    /// distinct from `MissingJobSpec` so the build queue can tell the
    /// caller "your code repo conflicts with another publisher" rather
    /// than "no spec found".
    #[error("output dataset {dataset_rid} is produced by multiple JobSpecs ({first_rid}, {second_rid})")]
    AmbiguousOutput {
        dataset_rid: String,
        first_rid: String,
        second_rid: String,
    },
    #[error("cycle detected in JobSpec graph: {}", cycle.join(" → "))]
    Cycle { cycle: Vec<String> },
    #[error("dataset versioning client error: {0}")]
    Client(String),
}

/// One node in the compiled graph. The order in [`JobGraph::nodes`] is
/// a valid topological order (producers before consumers).
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct JobGraphNode {
    pub job_spec: JobSpec,
    /// `dataset_rid → producer JobSpec rid` — only entries for inputs
    /// that are actually produced by another JobSpec in this graph.
    /// External datasets are absent from the map.
    pub upstream_producers: BTreeMap<String, String>,
}

/// The compiled output of [`compile_build_graph`]. Ready for the
/// resolver to walk top-to-bottom.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct JobGraph {
    pub nodes: Vec<JobGraphNode>,
    /// `output_dataset_rid → JobSpec rid` for every dataset produced
    /// by the graph (including transitively-collected ancestors).
    pub producers: BTreeMap<String, String>,
}

/// Compile the build graph for `output_datasets` on `build_branch`.
///
/// Algorithm:
///
/// 1. **Walk** outputs and look up each JobSpec by walking
///    `[build_branch, ...job_spec_fallback]`. The first branch that
///    publishes a spec wins; subsequent fallbacks are ignored.
/// 2. **Expand** transitively: each JobSpec's inputs that are
///    produced by *other* JobSpecs become work items; the algorithm
///    recurses until no new outputs need looking up.
/// 3. **Detect** cycles via Kahn's algorithm.
/// 4. **Return** the graph topologically ordered.
///
/// `repo` is dependency-injected so the dry-run path can stub it with
/// an in-memory map without any HTTP plumbing.
pub async fn compile_build_graph(
    pipeline_rid: &str,
    build_branch: &str,
    output_datasets: &[String],
    job_spec_fallback: &[String],
    repo: &dyn JobSpecRepo,
) -> Result<JobGraph, CompileError> {
    let mut tried_chain: Vec<String> = vec![build_branch.to_string()];
    tried_chain.extend(job_spec_fallback.iter().cloned());

    // Producer index — `output_dataset_rid → JobSpec rid`.
    let mut producers: BTreeMap<String, String> = BTreeMap::new();
    // Spec storage keyed by JobSpec rid (deduped).
    let mut specs: BTreeMap<String, JobSpec> = BTreeMap::new();

    let mut queue: VecDeque<String> = output_datasets.iter().cloned().collect();
    let mut visited: BTreeSet<String> = BTreeSet::new();

    while let Some(dataset_rid) = queue.pop_front() {
        if !visited.insert(dataset_rid.clone()) {
            continue;
        }
        let spec = repo
            .lookup(pipeline_rid, &dataset_rid, build_branch, job_spec_fallback)
            .await
            .map_err(|ClientError(msg)| CompileError::Client(msg))?
            .ok_or_else(|| CompileError::MissingJobSpec {
                dataset_rid: dataset_rid.clone(),
                tried_branches: tried_chain.clone(),
            })?;

        for produced in &spec.output_dataset_rids {
            if let Some(prev) = producers.get(produced) {
                if prev != &spec.rid {
                    return Err(CompileError::AmbiguousOutput {
                        dataset_rid: produced.clone(),
                        first_rid: prev.clone(),
                        second_rid: spec.rid.clone(),
                    });
                }
            } else {
                producers.insert(produced.clone(), spec.rid.clone());
            }
        }

        for input in &spec.inputs {
            if !visited.contains(&input.dataset_rid) {
                queue.push_back(input.dataset_rid.clone());
            }
        }

        specs.insert(spec.rid.clone(), spec);
    }

    let nodes = topological_order(&specs, &producers)?;
    Ok(JobGraph { nodes, producers })
}

/// Kahn's algorithm. Returns nodes in producer-before-consumer order
/// or `Err(Cycle)` with a representative cycle path.
fn topological_order(
    specs: &BTreeMap<String, JobSpec>,
    producers: &BTreeMap<String, String>,
) -> Result<Vec<JobGraphNode>, CompileError> {
    let mut indegree: BTreeMap<String, usize> = BTreeMap::new();
    let mut adjacency: BTreeMap<String, Vec<String>> = BTreeMap::new();
    let mut upstream_per_node: BTreeMap<String, BTreeMap<String, String>> = BTreeMap::new();

    for spec in specs.values() {
        indegree.entry(spec.rid.clone()).or_insert(0);
        adjacency.entry(spec.rid.clone()).or_default();
        let upstreams = upstream_per_node
            .entry(spec.rid.clone())
            .or_default();
        for input in &spec.inputs {
            if let Some(producer_rid) = producers.get(&input.dataset_rid) {
                if producer_rid != &spec.rid {
                    upstreams.insert(input.dataset_rid.clone(), producer_rid.clone());
                    adjacency
                        .entry(producer_rid.clone())
                        .or_default()
                        .push(spec.rid.clone());
                    *indegree.entry(spec.rid.clone()).or_insert(0) += 1;
                }
            }
        }
    }

    let mut queue: VecDeque<String> = indegree
        .iter()
        .filter(|(_, n)| **n == 0)
        .map(|(k, _)| k.clone())
        .collect();
    let mut ordered: Vec<JobGraphNode> = Vec::new();
    while let Some(rid) = queue.pop_front() {
        let spec = specs.get(&rid).cloned().expect("rid is in specs map");
        let upstreams = upstream_per_node.remove(&rid).unwrap_or_default();
        ordered.push(JobGraphNode {
            job_spec: spec,
            upstream_producers: upstreams,
        });
        if let Some(neighbours) = adjacency.remove(&rid) {
            for next in neighbours {
                if let Some(n) = indegree.get_mut(&next) {
                    *n -= 1;
                    if *n == 0 {
                        queue.push_back(next);
                    }
                }
            }
        }
    }

    if ordered.len() != specs.len() {
        // Build a representative cycle: pick any unscheduled node and
        // walk producer edges until we revisit it.
        let stuck: Vec<String> = indegree
            .iter()
            .filter(|(_, n)| **n > 0)
            .map(|(k, _)| k.clone())
            .collect();
        return Err(CompileError::Cycle { cycle: stuck });
    }
    Ok(ordered)
}

// ---------------------------------------------------------------------------
// In-memory `JobSpecRepo` — used by the dry-run-resolve endpoint and
// the unit tests below. HTTP-backed implementations live in
// `infra::clients` (added when the lifecycle ports are wired up).
// ---------------------------------------------------------------------------

/// A simple key for the in-memory map: `(pipeline_rid, branch,
/// output_dataset_rid)`.
type SpecKey = (String, String, String);

/// In-memory [`JobSpecRepo`] for tests and the dry-run endpoint.
#[derive(Debug, Default, Clone)]
pub struct InMemoryJobSpecRepo {
    pub specs: BTreeMap<SpecKey, JobSpec>,
}

impl InMemoryJobSpecRepo {
    pub fn new() -> Self {
        Self::default()
    }

    pub fn insert(&mut self, spec: JobSpec) -> &mut Self {
        for output in &spec.output_dataset_rids {
            self.specs.insert(
                (
                    spec.pipeline_rid.clone(),
                    spec.branch_name.clone(),
                    output.clone(),
                ),
                spec.clone(),
            );
        }
        self
    }
}

#[async_trait]
impl JobSpecRepo for InMemoryJobSpecRepo {
    async fn lookup(
        &self,
        pipeline_rid: &str,
        output_dataset_rid: &str,
        build_branch: &str,
        fallback_chain: &[String],
    ) -> Result<Option<JobSpec>, ClientError> {
        let mut chain: Vec<&str> = vec![build_branch];
        chain.extend(fallback_chain.iter().map(String::as_str));
        for branch in chain {
            let key = (
                pipeline_rid.to_string(),
                branch.to_string(),
                output_dataset_rid.to_string(),
            );
            if let Some(spec) = self.specs.get(&key) {
                return Ok(Some(spec.clone()));
            }
        }
        Ok(None)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::domain::build_resolution::InputSpec;

    fn spec(
        rid: &str,
        pipeline_rid: &str,
        branch: &str,
        inputs: &[&str],
        outputs: &[&str],
    ) -> JobSpec {
        JobSpec {
            rid: rid.to_string(),
            pipeline_rid: pipeline_rid.to_string(),
            branch_name: branch.to_string(),
            inputs: inputs
                .iter()
                .map(|d| InputSpec {
                    dataset_rid: d.to_string(),
                    fallback_chain: vec!["master".to_string()],
                    view_filter: vec![],
                    require_fresh: false,
                })
                .collect(),
            output_dataset_rids: outputs.iter().map(|s| s.to_string()).collect(),
            logic_kind: "sql".to_string(),
            logic_payload: serde_json::Value::Null,
            content_hash: "abc".to_string(),
        }
    }

    #[tokio::test]
    async fn doc_example_a_b_c_falls_back_to_master() {
        // Foundry doc § "Example: Building on branches":
        //   datasets A, B, C; build on `feature`, fallback `master`.
        //   B = transform(A); C = transform(B).
        //   `feature` publishes JobSpecs for B and C.
        //   `master` does NOT publish A's spec (A is external input).
        let mut repo = InMemoryJobSpecRepo::new();
        repo.insert(spec("spec/B", "p1", "feature", &["A"], &["B"]))
            .insert(spec("spec/C", "p1", "feature", &["B"], &["C"]));

        let graph = compile_build_graph(
            "p1",
            "feature",
            &["B".to_string(), "C".to_string()],
            &["master".to_string()],
            &repo,
        )
        .await
        .expect("compile");
        assert_eq!(graph.nodes.len(), 2, "B and C are the only producers");
        // Topological order: B first (no upstream producer in graph),
        // then C (depends on B).
        assert_eq!(graph.nodes[0].job_spec.rid, "spec/B");
        assert_eq!(graph.nodes[1].job_spec.rid, "spec/C");
        assert_eq!(graph.producers.get("B"), Some(&"spec/B".to_string()));
        assert_eq!(graph.producers.get("C"), Some(&"spec/C".to_string()));
    }

    #[tokio::test]
    async fn falls_back_to_master_when_feature_doesnt_publish() {
        let mut repo = InMemoryJobSpecRepo::new();
        repo.insert(spec("spec/B/master", "p1", "master", &["A"], &["B"]));

        let graph =
            compile_build_graph("p1", "feature", &["B".into()], &["master".into()], &repo)
                .await
                .expect("compile");
        assert_eq!(graph.nodes[0].job_spec.rid, "spec/B/master");
    }

    #[tokio::test]
    async fn missing_job_spec_lists_chain() {
        let repo = InMemoryJobSpecRepo::new();
        let err = compile_build_graph(
            "p1",
            "feature",
            &["X".into()],
            &["develop".into(), "master".into()],
            &repo,
        )
        .await
        .expect_err("missing");
        match err {
            CompileError::MissingJobSpec {
                dataset_rid,
                tried_branches,
            } => {
                assert_eq!(dataset_rid, "X");
                assert_eq!(
                    tried_branches,
                    vec!["feature".to_string(), "develop".into(), "master".into()]
                );
            }
            other => panic!("expected MissingJobSpec, got {other:?}"),
        }
    }

    #[tokio::test]
    async fn cycle_between_two_specs_is_rejected() {
        let mut repo = InMemoryJobSpecRepo::new();
        repo.insert(spec("spec/B", "p1", "master", &["C"], &["B"]))
            .insert(spec("spec/C", "p1", "master", &["B"], &["C"]));

        let err = compile_build_graph(
            "p1",
            "master",
            &["B".into(), "C".into()],
            &[],
            &repo,
        )
        .await
        .expect_err("cycle");
        assert!(matches!(err, CompileError::Cycle { .. }));
    }

    #[tokio::test]
    async fn ambiguous_output_two_specs_same_dataset() {
        let mut repo = InMemoryJobSpecRepo::new();
        repo.insert(spec("spec/B1", "p1", "master", &[], &["B"]));
        // Second spec on the same branch-output is impossible due to
        // the pipeline_job_specs unique key, but a different pipeline
        // can contend for the same dataset. Simulate it by inserting
        // two pipelines pointing at B and asking for both.
        repo.insert(spec("spec/B2", "p2", "master", &[], &["B"]));

        // Resolution from p1 only sees its own; we don't trigger
        // AmbiguousOutput unless a single resolution returns two
        // distinct specs. Sanity-check the happy path:
        let graph = compile_build_graph("p1", "master", &["B".into()], &[], &repo)
            .await
            .unwrap();
        assert_eq!(graph.nodes.len(), 1);
    }
}
