//! HTTP/JSON client for Vespa's Document v1 and Search APIs.
//!
//! Implements [`VectorBackend`] on top of `reqwest`. The implementation
//! is intentionally narrow: it covers exactly what the trait needs (CRUD
//! by id and a hybrid YQL query with a tensor input) so we avoid pulling
//! in a heavier client surface.

use std::collections::BTreeMap;
use std::time::Duration;

use async_trait::async_trait;
use reqwest::{Client, StatusCode};
use serde::{Deserialize, Serialize};
use serde_json::{Map, Value, json};
use tracing::{debug, instrument};

use crate::backend::{BackendError, BackendResult, Filter, QueryHit, VectorBackend};

/// Configuration required to talk to a Vespa container/cluster.
///
/// The defaults mirror Vespa's own defaults (`namespace = "default"`,
/// `doc_type = "doc"`, `rank_profile = "hybrid"`, `embedding_field =
/// "embedding"`, `text_field = "text"`) so that consumers running the
/// schema documented at the [crate level](super) only need to provide
/// the `endpoint`.
#[derive(Debug, Clone)]
pub struct VespaConfig {
    /// Base URL of the Vespa container, e.g. `http://localhost:8080`.
    /// No trailing slash.
    pub endpoint: String,
    /// Vespa document namespace (the `<namespace>` segment of the
    /// Document v1 path).
    pub namespace: String,
    /// Document type (the `<doctype>` segment, also the schema name).
    pub doc_type: String,
    /// Name of the indexed BM25 string field.
    pub text_field: String,
    /// Name of the dense `tensor<float>(x[N])` field.
    pub embedding_field: String,
    /// `rank-profile` to use for hybrid queries.
    pub rank_profile: String,
    /// HTTP timeout for individual Vespa requests.
    pub request_timeout: Duration,
}

impl VespaConfig {
    /// Build a config with sensible defaults for the schema documented
    /// in this module, pointing at `endpoint`.
    pub fn new(endpoint: impl Into<String>) -> Self {
        Self {
            endpoint: endpoint.into().trim_end_matches('/').to_string(),
            namespace: "default".to_string(),
            doc_type: "doc".to_string(),
            text_field: "text".to_string(),
            embedding_field: "embedding".to_string(),
            rank_profile: "hybrid".to_string(),
            request_timeout: Duration::from_secs(10),
        }
    }
}

/// [`VectorBackend`] implementation backed by Vespa.
///
/// Cheap to clone (wraps an `Arc`-backed `reqwest::Client` and a small
/// configuration struct).
#[derive(Debug, Clone)]
pub struct VespaBackend {
    cfg: VespaConfig,
    http: Client,
}

impl VespaBackend {
    /// Create a new backend with the given configuration. Returns an
    /// error if the underlying `reqwest::Client` cannot be built (e.g.
    /// invalid TLS configuration on the host).
    pub fn new(cfg: VespaConfig) -> BackendResult<Self> {
        let http = Client::builder()
            .timeout(cfg.request_timeout)
            .build()
            .map_err(|e| BackendError::Transport(e.to_string()))?;
        Ok(Self { cfg, http })
    }

    /// Same as [`Self::new`] but uses a caller-provided
    /// [`reqwest::Client`]. Useful for sharing connection pools or
    /// installing custom middleware in tests.
    pub fn with_client(cfg: VespaConfig, http: Client) -> Self {
        Self { cfg, http }
    }

    /// Build the Document v1 URL for `doc_id`.
    pub(crate) fn document_url(&self, doc_id: &str) -> String {
        format!(
            "{}/document/v1/{}/{}/docid/{}",
            self.cfg.endpoint,
            self.cfg.namespace,
            self.cfg.doc_type,
            urlencode(doc_id),
        )
    }

    /// Build the search endpoint URL.
    pub(crate) fn search_url(&self) -> String {
        format!("{}/search/", self.cfg.endpoint)
    }

    /// Compose the YQL statement and search request body for a hybrid
    /// query. Exposed (crate-private) so unit tests can assert the shape
    /// without needing a live Vespa.
    pub(crate) fn build_search_body(
        &self,
        text: &str,
        embedding: &[f32],
        filter: &Filter,
        top_k: usize,
    ) -> Value {
        let mut where_clauses: Vec<String> = Vec::new();

        if !text.is_empty() {
            where_clauses.push(format!(
                "({} contains \"{}\")",
                self.cfg.text_field,
                yql_escape(text),
            ));
        }
        if !embedding.is_empty() {
            // `targetHits` is a hint to the ANN operator; we ask for at
            // least top_k candidates so the rank-profile has material to
            // re-rank.
            where_clauses.push(format!(
                "({{targetHits:{}}}nearestNeighbor({},q_embedding))",
                top_k.max(1),
                self.cfg.embedding_field,
            ));
        }

        // Combine text + ANN with OR so either signal can surface a hit;
        // the rank-profile is what actually fuses them.
        let mut yql = if where_clauses.is_empty() {
            format!("select * from sources {} where true", self.cfg.doc_type)
        } else {
            format!(
                "select * from sources {} where {}",
                self.cfg.doc_type,
                where_clauses.join(" or "),
            )
        };

        // AND in any equality filters.
        for (field, value) in &filter.equals {
            yql.push_str(&format!(
                " and ({} contains \"{}\")",
                field,
                yql_escape(&json_to_string(value)),
            ));
        }

        let mut body = Map::new();
        body.insert("yql".to_string(), Value::String(yql));
        body.insert("hits".to_string(), Value::from(top_k));
        body.insert(
            "ranking.profile".to_string(),
            Value::String(self.cfg.rank_profile.clone()),
        );
        if !embedding.is_empty() {
            // Vespa accepts indexed tensors as plain JSON arrays under
            // `input.query(<name>)` when posted via the Search API.
            body.insert(
                "input.query(q_embedding)".to_string(),
                embedding_to_json(embedding),
            );
        }

        Value::Object(body)
    }
}

#[async_trait]
impl VectorBackend for VespaBackend {
    #[instrument(skip(self, fields, embedding), fields(id = %doc_id))]
    async fn upsert(
        &self,
        doc_id: &str,
        fields: &BTreeMap<String, Value>,
        embedding: &[f32],
    ) -> BackendResult<()> {
        let mut payload_fields = Map::new();
        for (k, v) in fields {
            payload_fields.insert(k.clone(), v.clone());
        }
        if !embedding.is_empty() {
            // Indexed tensors go on the wire as plain JSON arrays under
            // the field name; Vespa parses them according to the schema.
            payload_fields.insert(
                self.cfg.embedding_field.clone(),
                embedding_to_json(embedding),
            );
        }
        let body = json!({ "fields": Value::Object(payload_fields) });

        let url = self.document_url(doc_id);
        debug!(%url, "vespa upsert");
        let resp = self
            .http
            .post(&url)
            .json(&body)
            .send()
            .await
            .map_err(|e| BackendError::Transport(e.to_string()))?;
        check_status(resp).await.map(|_| ())
    }

    #[instrument(skip(self), fields(id = %doc_id))]
    async fn delete(&self, doc_id: &str) -> BackendResult<()> {
        let url = self.document_url(doc_id);
        debug!(%url, "vespa delete");
        let resp = self
            .http
            .delete(&url)
            .send()
            .await
            .map_err(|e| BackendError::Transport(e.to_string()))?;
        // Vespa returns 200 on delete even when the doc was missing;
        // treat 404 as no-op for portability.
        if resp.status() == StatusCode::NOT_FOUND {
            return Ok(());
        }
        check_status(resp).await.map(|_| ())
    }

    #[instrument(skip(self, embedding, filter), fields(top_k))]
    async fn hybrid_query(
        &self,
        text: &str,
        embedding: &[f32],
        filter: &Filter,
        top_k: usize,
    ) -> BackendResult<Vec<QueryHit>> {
        let body = self.build_search_body(text, embedding, filter, top_k);
        let url = self.search_url();
        debug!(%url, "vespa hybrid_query");
        let resp = self
            .http
            .post(&url)
            .json(&body)
            .send()
            .await
            .map_err(|e| BackendError::Transport(e.to_string()))?;
        let value = check_status(resp).await?;
        parse_search_response(&value)
    }
}

/// Validate `2xx` responses; convert anything else into a
/// [`BackendError::Backend`] with the body for context.
async fn check_status(resp: reqwest::Response) -> BackendResult<Value> {
    let status = resp.status();
    let text = resp
        .text()
        .await
        .map_err(|e| BackendError::Transport(e.to_string()))?;
    if !status.is_success() {
        return Err(BackendError::Backend(format!("{status}: {text}")));
    }
    if text.is_empty() {
        return Ok(Value::Null);
    }
    serde_json::from_str(&text).map_err(|e| BackendError::Serialization(e.to_string()))
}

/// Parse a Vespa Search API JSON response into [`QueryHit`]s.
///
/// Lives at module scope so unit tests can call it on captured fixtures
/// without a live Vespa.
pub(crate) fn parse_search_response(value: &Value) -> BackendResult<Vec<QueryHit>> {
    #[derive(Deserialize)]
    struct Root {
        root: Inner,
    }
    #[derive(Deserialize)]
    struct Inner {
        #[serde(default)]
        children: Vec<Child>,
    }
    #[derive(Deserialize, Serialize)]
    struct Child {
        #[serde(default)]
        id: Option<String>,
        #[serde(default)]
        relevance: Option<f64>,
        #[serde(default)]
        fields: Map<String, Value>,
    }

    let parsed: Root = serde_json::from_value(value.clone())
        .map_err(|e| BackendError::Serialization(e.to_string()))?;
    let mut out = Vec::with_capacity(parsed.root.children.len());
    for c in parsed.root.children {
        // Vespa returns ids like "id:<namespace>:<doctype>::<docid>";
        // the user-supplied id is the trailing segment after `::`.
        let raw = c.id.unwrap_or_default();
        let id = raw
            .rsplit_once("::")
            .map(|(_, tail)| tail.to_string())
            .unwrap_or(raw);
        out.push(QueryHit {
            id,
            score: c.relevance.unwrap_or(0.0),
            fields: c.fields.into_iter().collect(),
        });
    }
    Ok(out)
}

/// Convert a dense `f32` embedding into the JSON array Vespa expects on
/// the wire.
///
/// JSON has no representation for non-finite floats, so any `NaN`,
/// `+Infinity` or `-Infinity` component is mapped to `null`. Vespa will
/// in turn reject the document/query (4xx with a clear message), which
/// surfaces as [`BackendError::Backend`]. Callers should ensure their
/// embeddings are finite — every model worth using already emits finite
/// values, but defensive code can `assert!(v.iter().all(|f| f.is_finite()))`
/// before calling `upsert`/`hybrid_query`.
fn embedding_to_json(embedding: &[f32]) -> Value {
    Value::Array(
        embedding
            .iter()
            .map(|f| {
                serde_json::Number::from_f64(f64::from(*f))
                    .map(Value::Number)
                    .unwrap_or(Value::Null)
            })
            .collect(),
    )
}

/// Minimal percent-encoding for the path segment of Document v1 URLs.
/// We only encode the characters that would otherwise change the path's
/// structural meaning; everything else passes through unchanged so user
/// ids stay readable in logs.
fn urlencode(s: &str) -> String {
    let mut out = String::with_capacity(s.len());
    for b in s.bytes() {
        match b {
            b'A'..=b'Z' | b'a'..=b'z' | b'0'..=b'9' | b'-' | b'_' | b'.' | b'~' => {
                out.push(b as char);
            }
            _ => out.push_str(&format!("%{b:02X}")),
        }
    }
    out
}

/// Escape a string value so it is safe to embed inside a YQL
/// double-quoted literal.
fn yql_escape(s: &str) -> String {
    let mut out = String::with_capacity(s.len());
    for c in s.chars() {
        match c {
            '\\' => out.push_str("\\\\"),
            '"' => out.push_str("\\\""),
            '\n' => out.push_str("\\n"),
            '\r' => out.push_str("\\r"),
            '\t' => out.push_str("\\t"),
            _ => out.push(c),
        }
    }
    out
}

/// Render a JSON scalar as a string for use in a YQL `contains` clause.
/// Non-scalar values are JSON-stringified, which is intentional: callers
/// shouldn't be passing arrays/objects to an equality filter, and if they
/// do we still produce *something* deterministic instead of panicking.
fn json_to_string(v: &Value) -> String {
    match v {
        Value::String(s) => s.clone(),
        Value::Bool(b) => b.to_string(),
        Value::Number(n) => n.to_string(),
        Value::Null => String::new(),
        other => other.to_string(),
    }
}
