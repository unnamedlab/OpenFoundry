//! OpenSearch-backed [`SearchBackend`].
//!
//! OpenSearch is the dev / CI fallback per
//! [ADR-0028](../../docs/architecture/adr/ADR-0028-search-backend-abstraction.md).
//!
//! Wire format:
//!
//! * Index per `(tenant, type_id)` named `of-{tenant}-{type}`
//!   (sanitized, lowercase).
//! * `PUT  /{index}/_doc/{id}?if_seq_no=…&if_primary_term=…` — we
//!   use the `version` field with `version_type=external` so stale
//!   writes are silently dropped (HTTP 409).
//! * `POST /{index}/_search` for both lexical (`query_string`) and
//!   vector (`knn`) queries.
//! * `POST /_bulk` for `bulk_index`.
//!
//! We deliberately do **not** use the official `opensearch` crate.
//! Its dependency footprint (aws-sigv4, hyper-tls, …) is heavier
//! than the surface we exercise; raw `reqwest` against the REST API
//! keeps the build small and the wire format auditable in code
//! review.

use async_trait::async_trait;
use serde_json::{Value, json};

use crate::{
    BulkOutcome, IndexDoc, ObjectId, PagedResult, ReadConsistency, RepoError, RepoResult,
    SearchBackend, SearchHit, SearchQuery, TenantId, TypeId, VectorQuery, sanitize_doc_type,
};

/// OpenSearch client. Construct with the cluster's HTTP endpoint
/// (typically `http://opensearch:9200` in `compose dev`).
pub struct OpenSearchBackend {
    endpoint: String,
    http: reqwest::Client,
}

impl OpenSearchBackend {
    /// Build with an explicit endpoint and a default `reqwest::Client`.
    pub fn new(endpoint: impl Into<String>) -> Self {
        Self::with_client(endpoint, reqwest::Client::new())
    }

    /// Build with a caller-provided HTTP client.
    pub fn with_client(endpoint: impl Into<String>, http: reqwest::Client) -> Self {
        let mut endpoint = endpoint.into();
        if endpoint.ends_with('/') {
            endpoint.pop();
        }
        Self { endpoint, http }
    }

    fn index_name(&self, tenant: &TenantId, type_id: &TypeId) -> String {
        format!(
            "of-{}-{}",
            sanitize_index(&tenant.0),
            sanitize_doc_type(&type_id.0)
        )
    }

    fn doc_url(&self, index: &str, id: &ObjectId) -> String {
        format!("{}/{}/_doc/{}", self.endpoint, index, urlencode(&id.0))
    }
}

fn sanitize_index(s: &str) -> String {
    s.to_ascii_lowercase()
        .chars()
        .map(|c| {
            if c.is_ascii_alphanumeric() || c == '-' || c == '_' {
                c
            } else {
                '_'
            }
        })
        .collect()
}

fn urlencode(s: &str) -> String {
    s.bytes()
        .map(|b| match b {
            b'A'..=b'Z' | b'a'..=b'z' | b'0'..=b'9' | b'-' | b'_' | b'.' | b'~' => {
                (b as char).to_string()
            }
            other => format!("%{:02X}", other),
        })
        .collect()
}

fn map_status(status: reqwest::StatusCode, ctx: &'static str) -> Option<RepoError> {
    if status.is_success() {
        None
    } else {
        Some(RepoError::Backend(format!(
            "opensearch {ctx} HTTP {}: {}",
            status.as_u16(),
            status.canonical_reason().unwrap_or("")
        )))
    }
}

#[async_trait]
impl SearchBackend for OpenSearchBackend {
    async fn search(
        &self,
        query: SearchQuery,
        _consistency: ReadConsistency,
    ) -> RepoResult<PagedResult<SearchHit>> {
        // Pick index: when `type_id` is unspecified we hit the
        // tenant-wide alias (`of-{tenant}-*`); when present we
        // target the concrete index for a tighter shard scan.
        let target = match &query.type_id {
            Some(t) => self.index_name(&query.tenant, t),
            None => format!("of-{}-*", sanitize_index(&query.tenant.0)),
        };
        let url = format!("{}/{}/_search", self.endpoint, target);

        let mut filter: Vec<Value> = vec![json!({"term": {"tenant": query.tenant.0}})];
        if let Some(t) = &query.type_id {
            filter.push(json!({"term": {"type_id": t.0}}));
        }
        for (k, v) in &query.filters {
            filter.push(json!({"term": {k: v}}));
        }

        let mut must: Vec<Value> = Vec::new();
        if let Some(q) = &query.q {
            if !q.is_empty() {
                must.push(json!({"query_string": {"query": q}}));
            }
        }
        if must.is_empty() {
            must.push(json!({"match_all": {}}));
        }

        let body = json!({
            "size": query.page.size.max(1),
            "query": {"bool": {"must": must, "filter": filter}},
        });

        let resp = self
            .http
            .post(url)
            .json(&body)
            .send()
            .await
            .map_err(|e| RepoError::Backend(format!("opensearch search send: {e}")))?;
        if resp.status().as_u16() == 404 {
            return Ok(PagedResult {
                items: vec![],
                next_token: None,
            });
        }
        if let Some(err) = map_status(resp.status(), "search") {
            return Err(err);
        }
        let v: Value = resp
            .json()
            .await
            .map_err(|e| RepoError::Backend(format!("opensearch search decode: {e}")))?;
        Ok(PagedResult {
            items: parse_hits(&v),
            next_token: None,
        })
    }

    async fn index(&self, doc: IndexDoc) -> RepoResult<()> {
        let index = self.index_name(&doc.tenant, &doc.type_id);
        let url = self.doc_url(&index, &doc.id);
        let mut payload = match doc.payload.clone() {
            Value::Object(map) => Value::Object(map),
            other => json!({ "payload": other }),
        };
        if let Some(map) = payload.as_object_mut() {
            map.insert("id".into(), json!(doc.id.0));
            map.insert("tenant".into(), json!(doc.tenant.0));
            map.insert("type_id".into(), json!(doc.type_id.0));
            map.insert("version".into(), json!(doc.version));
            if let Some(emb) = &doc.embedding {
                map.insert("embedding".into(), json!(emb));
            }
        }

        let resp = self
            .http
            .put(url)
            .query(&[
                ("version", doc.version.to_string().as_str()),
                ("version_type", "external"),
            ])
            .json(&payload)
            .send()
            .await
            .map_err(|e| RepoError::Backend(format!("opensearch index send: {e}")))?;
        // 409 = stale (external version conflict) — silently drop.
        if resp.status().as_u16() == 409 {
            return Ok(());
        }
        if let Some(err) = map_status(resp.status(), "index") {
            return Err(err);
        }
        Ok(())
    }

    async fn delete(&self, tenant: &TenantId, id: &ObjectId) -> RepoResult<bool> {
        // Like Vespa, we don't know the type_id; use a wildcard
        // delete-by-query.
        let target = format!("of-{}-*", sanitize_index(&tenant.0));
        let url = format!("{}/{}/_delete_by_query", self.endpoint, target);
        let body = json!({
            "query": {
                "bool": {
                    "filter": [
                        {"term": {"tenant": tenant.0}},
                        {"term": {"id": id.0}},
                    ]
                }
            }
        });
        let resp = self
            .http
            .post(url)
            .json(&body)
            .send()
            .await
            .map_err(|e| RepoError::Backend(format!("opensearch delete send: {e}")))?;
        if resp.status().as_u16() == 404 {
            return Ok(false);
        }
        if let Some(err) = map_status(resp.status(), "delete") {
            return Err(err);
        }
        let v: Value = resp
            .json()
            .await
            .map_err(|e| RepoError::Backend(format!("opensearch delete decode: {e}")))?;
        let deleted = v.get("deleted").and_then(|x| x.as_u64()).unwrap_or(0);
        Ok(deleted > 0)
    }

    async fn search_vector(
        &self,
        query: VectorQuery,
        _consistency: ReadConsistency,
    ) -> RepoResult<Vec<SearchHit>> {
        let target = match &query.type_id {
            Some(t) => self.index_name(&query.tenant, t),
            None => format!("of-{}-*", sanitize_index(&query.tenant.0)),
        };
        let url = format!("{}/{}/_search", self.endpoint, target);
        let mut filter: Vec<Value> = vec![json!({"term": {"tenant": query.tenant.0}})];
        if let Some(t) = &query.type_id {
            filter.push(json!({"term": {"type_id": t.0}}));
        }
        for (k, v) in &query.filters {
            filter.push(json!({"term": {k: v}}));
        }
        let body = json!({
            "size": query.k.max(1),
            "query": {
                "knn": {
                    "embedding": {
                        "vector": query.embedding,
                        "k": query.k.max(1),
                        "filter": {"bool": {"filter": filter}}
                    }
                }
            }
        });
        let resp = self
            .http
            .post(url)
            .json(&body)
            .send()
            .await
            .map_err(|e| RepoError::Backend(format!("opensearch vector send: {e}")))?;
        if resp.status().as_u16() == 404 {
            return Ok(vec![]);
        }
        if let Some(err) = map_status(resp.status(), "search_vector") {
            return Err(err);
        }
        let v: Value = resp
            .json()
            .await
            .map_err(|e| RepoError::Backend(format!("opensearch vector decode: {e}")))?;
        Ok(parse_hits(&v))
    }

    async fn bulk_index(&self, docs: Vec<IndexDoc>) -> RepoResult<BulkOutcome> {
        if docs.is_empty() {
            return Ok(BulkOutcome::default());
        }
        let url = format!("{}/_bulk", self.endpoint);
        let mut body = String::new();
        for d in &docs {
            let index = self.index_name(&d.tenant, &d.type_id);
            let header = json!({
                "index": {
                    "_index": index,
                    "_id": d.id.0,
                    "version": d.version,
                    "version_type": "external",
                }
            });
            let mut payload = match d.payload.clone() {
                Value::Object(map) => Value::Object(map),
                other => json!({ "payload": other }),
            };
            if let Some(map) = payload.as_object_mut() {
                map.insert("id".into(), json!(d.id.0));
                map.insert("tenant".into(), json!(d.tenant.0));
                map.insert("type_id".into(), json!(d.type_id.0));
                map.insert("version".into(), json!(d.version));
                if let Some(emb) = &d.embedding {
                    map.insert("embedding".into(), json!(emb));
                }
            }
            body.push_str(&serde_json::to_string(&header).unwrap());
            body.push('\n');
            body.push_str(&serde_json::to_string(&payload).unwrap());
            body.push('\n');
        }
        let resp = self
            .http
            .post(url)
            .header("Content-Type", "application/x-ndjson")
            .body(body)
            .send()
            .await
            .map_err(|e| RepoError::Backend(format!("opensearch bulk send: {e}")))?;
        if let Some(err) = map_status(resp.status(), "bulk_index") {
            return Err(err);
        }
        let v: Value = resp
            .json()
            .await
            .map_err(|e| RepoError::Backend(format!("opensearch bulk decode: {e}")))?;
        let mut out = BulkOutcome::default();
        if let Some(items) = v.get("items").and_then(|x| x.as_array()) {
            for (i, entry) in items.iter().enumerate() {
                let result = entry.get("index").or_else(|| entry.get("create"));
                let status = result
                    .and_then(|r| r.get("status"))
                    .and_then(|s| s.as_u64());
                let id = docs
                    .get(i)
                    .map(|d| d.id.clone())
                    .unwrap_or(ObjectId(String::new()));
                match status {
                    Some(s) if (200..300).contains(&s) || s == 409 => out.indexed += 1,
                    Some(s) => out.failed.push((id, format!("status {s}"))),
                    None => out.failed.push((id, "missing status".into())),
                }
            }
        }
        Ok(out)
    }
}

fn parse_hits(v: &Value) -> Vec<SearchHit> {
    let Some(hits) = v
        .get("hits")
        .and_then(|h| h.get("hits"))
        .and_then(|h| h.as_array())
    else {
        return Vec::new();
    };
    hits.iter()
        .filter_map(|h| {
            let src = h.get("_source")?;
            let id = src
                .get("id")
                .and_then(|x| x.as_str())
                .or_else(|| h.get("_id").and_then(|x| x.as_str()))?
                .to_string();
            let type_id = src
                .get("type_id")
                .and_then(|x| x.as_str())
                .unwrap_or("")
                .to_string();
            let score = h.get("_score").and_then(|x| x.as_f64()).unwrap_or(0.0) as f32;
            Some(SearchHit {
                id: ObjectId(id),
                type_id: TypeId(type_id),
                score,
                snippet: Some(src.clone()),
            })
        })
        .collect()
}
