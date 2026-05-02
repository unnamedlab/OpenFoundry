//! Effective marking computation (T3.2).
//!
//! ## Background
//!
//! A dataset's *effective markings* are the union of:
//!
//!   1. **Direct** markings — rows in `dataset_markings` with
//!      `source = 'direct'` for that `dataset_rid`.
//!   2. **Inherited** markings — for every dataset upstream of the
//!      target (along the lineage graph), the union of *its* effective
//!      markings, re-tagged with `MarkingSource::InheritedFromUpstream
//!      { upstream_rid }` so the UI can answer "where does this come
//!      from?" without re-traversing the graph.
//!
//! The Datasets / Lineage docs require markings to inherit upstream so
//! a dataset that derives from `RESTRICTED` is automatically
//! `RESTRICTED` even if no human attached the label.
//!
//! ## Caching
//!
//! Lineage walks are read-mostly and expensive (cross-service HTTP +
//! recursive expansion), so we cache results in a [`moka`] in-memory
//! TTL cache (60 s). The cache is invalidated on demand via
//! [`MarkingResolver::invalidate`] — wired to the
//! `event-bus-control` subscriber in `main.rs` so any service can
//! publish a `dataset.markings.changed` event and have every replica
//! drop its stale entry.
//!
//! ## Why a trait
//!
//! `LineageClient` abstracts over "give me the upstream RIDs of X".
//! Production wires it to the HTTP call to `lineage-service`
//! (`GET /v1/lineage/{rid}/upstream`); tests use an in-memory `HashMap`
//! so the marking-propagation logic can be exercised without spinning
//! up a second service or a database.
//!
//! ## History
//!
//! Earlier revisions derived a single string marking from the dataset's
//! tags (`marking:pii`, `classification:confidential`, …) via the
//! `marking_from_tags` / `migrate_legacy_tag_markings` helpers. T9.1
//! removed those: `dataset_markings` is now the single source of truth
//! and effective markings flow exclusively through
//! [`MarkingResolver::compute`].

use std::{collections::HashSet, sync::Arc, time::Duration};

use async_trait::async_trait;
use core_models::security::{EffectiveMarking, MarkingId, MarkingSource};
use moka::future::Cache;
use serde::Deserialize;
use sqlx::PgPool;

/// Errors produced while computing effective markings.
#[derive(Debug, thiserror::Error)]
pub enum MarkingResolveError {
    #[error("database error: {0}")]
    Database(String),
    #[error("lineage lookup failed for {rid}: {source}")]
    Lineage {
        rid: String,
        #[source]
        source: anyhow::Error,
    },
    #[error("lineage cycle detected at {rid}")]
    Cycle { rid: String },
}

impl From<sqlx::Error> for MarkingResolveError {
    fn from(e: sqlx::Error) -> Self {
        Self::Database(e.to_string())
    }
}

/// Pluggable lineage lookup. Production implementation calls
/// `GET {lineage_service}/v1/lineage/{rid}/upstream`; tests use
/// [`InMemoryLineageClient`].
#[async_trait]
pub trait LineageClient: Send + Sync {
    /// Returns the immediate upstream RIDs of `dataset_rid`. Empty
    /// when the dataset is a source (no parents).
    async fn upstream(&self, dataset_rid: &str) -> anyhow::Result<Vec<String>>;
}

/// HTTP-backed [`LineageClient`] that talks to `lineage-service`.
pub struct HttpLineageClient {
    base_url: String,
    http: reqwest::Client,
}

impl HttpLineageClient {
    pub fn new(base_url: impl Into<String>, http: reqwest::Client) -> Self {
        Self {
            base_url: base_url.into().trim_end_matches('/').to_owned(),
            http,
        }
    }
}

#[derive(Debug, Deserialize)]
struct UpstreamResponse {
    #[serde(default)]
    upstream: Vec<String>,
}

#[async_trait]
impl LineageClient for HttpLineageClient {
    async fn upstream(&self, dataset_rid: &str) -> anyhow::Result<Vec<String>> {
        let url = format!(
            "{}/v1/lineage/{}/upstream",
            self.base_url,
            urlencoding::encode(dataset_rid)
        );
        let resp = self
            .http
            .get(&url)
            .send()
            .await
            .map_err(|e| anyhow::anyhow!("lineage GET {url}: {e}"))?;
        if !resp.status().is_success() {
            return Err(anyhow::anyhow!(
                "lineage GET {url} returned {}",
                resp.status()
            ));
        }
        let body: UpstreamResponse = resp
            .json()
            .await
            .map_err(|e| anyhow::anyhow!("lineage GET {url} body: {e}"))?;
        Ok(body.upstream)
    }
}

/// In-memory client used by tests.
#[derive(Default, Clone)]
pub struct InMemoryLineageClient {
    /// Map: child RID → its immediate upstream parents.
    pub edges: std::collections::HashMap<String, Vec<String>>,
}

#[async_trait]
impl LineageClient for InMemoryLineageClient {
    async fn upstream(&self, dataset_rid: &str) -> anyhow::Result<Vec<String>> {
        Ok(self.edges.get(dataset_rid).cloned().unwrap_or_default())
    }
}

/// Caching resolver. One instance per service process.
pub struct MarkingResolver {
    db: PgPool,
    lineage: Arc<dyn LineageClient>,
    cache: Cache<String, Arc<Vec<EffectiveMarking>>>,
}

impl MarkingResolver {
    /// Build with the default 60 s TTL recommended by the spec.
    pub fn new(db: PgPool, lineage: Arc<dyn LineageClient>) -> Self {
        Self::with_ttl(db, lineage, Duration::from_secs(60))
    }

    pub fn with_ttl(db: PgPool, lineage: Arc<dyn LineageClient>, ttl: Duration) -> Self {
        let cache = Cache::builder()
            .time_to_live(ttl)
            .max_capacity(10_000)
            .build();
        Self { db, lineage, cache }
    }

    /// Compute the effective markings for `dataset_rid`. Cached.
    pub async fn compute(
        &self,
        dataset_rid: &str,
    ) -> Result<Arc<Vec<EffectiveMarking>>, MarkingResolveError> {
        if let Some(hit) = self.cache.get(dataset_rid).await {
            return Ok(hit);
        }
        let mut visiting = HashSet::new();
        let computed = self.compute_inner(dataset_rid, &mut visiting).await?;
        let arc = Arc::new(computed);
        self.cache.insert(dataset_rid.to_string(), arc.clone()).await;
        Ok(arc)
    }

    /// Drop a single dataset's cached entry. Wired to the event bus.
    pub async fn invalidate(&self, dataset_rid: &str) {
        self.cache.invalidate(dataset_rid).await;
    }

    /// Drop everything. Useful on shutdown / tests.
    pub fn invalidate_all(&self) {
        self.cache.invalidate_all();
    }

    async fn compute_inner(
        &self,
        rid: &str,
        visiting: &mut HashSet<String>,
    ) -> Result<Vec<EffectiveMarking>, MarkingResolveError> {
        if !visiting.insert(rid.to_string()) {
            return Err(MarkingResolveError::Cycle {
                rid: rid.to_string(),
            });
        }

        // 1. Direct markings on this dataset.
        let mut effective = load_direct_markings(&self.db, rid).await?;

        // 2. Walk one hop upstream and union *their* effective markings,
        //    rebranded as inherited from each immediate parent.
        let parents = self
            .lineage
            .upstream(rid)
            .await
            .map_err(|source| MarkingResolveError::Lineage {
                rid: rid.to_string(),
                source,
            })?;
        for parent in parents {
            // Boxed recursion — async fn in trait without Box would
            // require `async-recursion`; the depth is bounded by the
            // lineage graph height (typically <20 in practice).
            let parent_marks =
                Box::pin(self.compute_inner(&parent, visiting)).await?;
            for marking in parent_marks {
                // Re-tag with the immediate upstream we crossed (not
                // the original `marking.source`); the closest hop is
                // what the UI shows.
                effective.push(EffectiveMarking {
                    id: marking.id,
                    source: MarkingSource::inherited_from(&parent),
                });
            }
        }

        visiting.remove(rid);
        Ok(dedupe(effective))
    }
}

/// Load only the rows where `source = 'direct'` (the local contribution
/// for this dataset). Inherited rows in the table are denormalised
/// projections of upstream — they're recomputed on the fly so a single
/// upstream change naturally invalidates everything via the cache.
async fn load_direct_markings(
    db: &PgPool,
    dataset_rid: &str,
) -> Result<Vec<EffectiveMarking>, MarkingResolveError> {
    let rows: Vec<(uuid::Uuid,)> = sqlx::query_as(
        r#"SELECT marking_id FROM dataset_markings
            WHERE dataset_rid = $1 AND source = 'direct'"#,
    )
    .bind(dataset_rid)
    .fetch_all(db)
    .await?;
    Ok(rows
        .into_iter()
        .map(|(id,)| EffectiveMarking::direct(MarkingId::from_uuid(id)))
        .collect())
}

/// Dedupe by `(id, source)` while preserving insertion order so the
/// caller sees direct markings before inherited ones.
fn dedupe(markings: Vec<EffectiveMarking>) -> Vec<EffectiveMarking> {
    let mut seen = HashSet::with_capacity(markings.len());
    markings
        .into_iter()
        .filter(|m| seen.insert((m.id, m.source.clone())))
        .collect()
}

// ───────────────────────────────────────────────────────────────────────
// Legacy compatibility helpers were removed in T9.1 (Bloque 9). The
// pre-T3.2 design derived a single "marking" string from a dataset's
// tag list (`marking:pii`, `classification:confidential`, …). All
// internal call sites now read directly from `dataset_markings` rows
// (`source = 'direct'`) and resolve effective markings via
// [`MarkingResolver::compute`].
// ───────────────────────────────────────────────────────────────────────


#[cfg(test)]
mod tests {
    use super::*;
    use std::collections::HashMap;

    fn id() -> MarkingId {
        MarkingId::new()
    }

    #[test]
    fn dedupe_collapses_identical_pairs_and_keeps_distinct_sources() {
        let m = id();
        let dup = vec![
            EffectiveMarking::direct(m),
            EffectiveMarking::direct(m),
            EffectiveMarking::inherited(m, "ri.up"),
            EffectiveMarking::inherited(m, "ri.up"),
            EffectiveMarking::inherited(m, "ri.other"),
        ];
        let out = dedupe(dup);
        assert_eq!(out.len(), 3, "{out:?}");
    }

    #[tokio::test]
    async fn in_memory_lineage_returns_configured_parents() {
        let mut edges = HashMap::new();
        edges.insert("ri.b".to_string(), vec!["ri.a".to_string()]);
        let client = InMemoryLineageClient { edges };
        assert_eq!(client.upstream("ri.b").await.unwrap(), vec!["ri.a".to_string()]);
        assert!(client.upstream("ri.unknown").await.unwrap().is_empty());
    }

    // The full inheritance chain is exercised in the integration test
    // `tests/marking_inheritance.rs`, which spins up a Postgres pool
    // via `sqlx::test`; the unit suite above stays pure / no-DB so it
    // can run on every laptop without a DB connection.
}
