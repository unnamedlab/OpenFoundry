//! Vespa-backed [`SearchBackend`].
//!
//! Vespa is the production search target per
//! [ADR-0028](../../docs/architecture/adr/ADR-0028-search-backend-abstraction.md).
//!
//! Wire format:
//!
//! * **Document API** —
//!   `PUT  /document/v1/{namespace}/{type}/group/{tenant}/{id}` with
//!   body `{"fields": { …payload, "tenant", "version", "embedding": {"values": […] } } }`.
//! * **Search API** —
//!   `POST /search/` with body `{"yql": "…", "hits": N, …}`.
//!   Vector search uses `nearestNeighbor(embedding, q)` with the
//!   query tensor passed via `input.query(q)`.
//!
//! Stale-write protection uses Vespa's `condition` parameter
//! (`condition={type}.version<{N}` ⇒ HTTP 412 on out-of-order PUT,
//! which we silently treat as a no-op).

use async_trait::async_trait;
use serde_json::{json, Value};

use crate::{
    sanitize_doc_type, BulkOutcome, IndexDoc, ObjectId, Page, PagedResult, ReadConsistency,
    RepoError, RepoResult, SearchBackend, SearchHit, SearchQuery, TenantId, TypeId, VectorQuery,
};

/// Vespa client. Construct with the cluster's HTTP endpoint
/// (typically `https://vespa.search.svc.cluster.local:8080`).
pub struct VespaSearchBackend {
    endpoint: String,
    namespace: String,
    http: reqwest::Client,
}

impl VespaSearchBackend {
    /// Build with an explicit endpoint and a default client.
    pub fn new(endpoint: impl Into<String>) -> Self {
        Self::with_client(endpoint, "of", reqwest::Client::new())
    }

    /// Build with a caller-provided HTTP client and namespace.
    pub fn with_client(
        endpoint: impl Into<String>,
        namespace: impl Into<String>,
        http: reqwest::Client,
    ) -> Self {
        let mut endpoint = endpoint.into();
        if endpoint.ends_with('/') {
            endpoint.pop();
        }
        Self {
            endpoint,
            namespace: namespace.into(),
            http,
        }
    }

    fn doc_url(&self, doc_type: &str, tenant: &TenantId, id: &ObjectId) -> String {
        format!(
            "{}/document/v1/{}/{}/group/{}/{}",
            self.endpoint,
            self.namespace,
            doc_type,
            urlencode(&tenant.0),
            urlencode(&id.0),
        )
    }

    fn search_url(&self) -> String {
        format!("{}/search/", self.endpoint)
    }

    fn build_yql(&self, query: &SearchQuery) -> String {
        let mut where_clauses: Vec<String> = vec![format!(
            "tenant contains \"{}\"",
            yql_escape(&query.tenant.0)
        )];
        if let Some(t) = &query.type_id {
            where_clauses.push(format!("type_id contains \"{}\"", yql_escape(&t.0)));
        }
        for (k, v) in &query.filters {
            where_clauses.push(format!(
                "{} contains \"{}\"",
                sanitize_field(k),
                yql_escape(v)
            ));
        }
        if query.q.as_deref().map(|q| !q.is_empty()).unwrap_or(false) {
            where_clauses.push("userQuery()".into());
        }
        let source = query
            .type_id
            .as_ref()
            .map(|t| sanitize_doc_type(&t.0))
            .unwrap_or_else(|| "*".into());
        format!(
            "select * from sources {} where {} limit {}",
            source,
            where_clauses.join(" and "),
            query.page.size.max(1)
        )
    }
}

fn yql_escape(s: &str) -> String {
    s.replace('\\', "\\\\").replace('"', "\\\"")
}

fn sanitize_field(s: &str) -> String {
    s.chars()
        .map(|c| if c.is_ascii_alphanumeric() || c == '_' { c } else { '_' })
        .collect()
}

fn urlencode(s: &str) -> String {
    // Minimal percent-encoding for path segments. We do not depend on
    // `urlencoding` to avoid one more crate; the inputs we encode are
    // tenant ids and object ids which are URL-safe in practice.
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
            "vespa {ctx} HTTP {}: {}",
            status.as_u16(),
            status.canonical_reason().unwrap_or("")
        )))
    }
}

#[async_trait]
impl SearchBackend for VespaSearchBackend {
    async fn search(
        &self,
        query: SearchQuery,
        _consistency: ReadConsistency,
    ) -> RepoResult<PagedResult<SearchHit>> {
        let q_text = query.q.clone();
        let mut body = json!({
            "yql": self.build_yql(&query),
            "hits": query.page.size.max(1),
        });
        if let Some(q) = q_text {
            body["query"] = json!(q);
        }
        let resp = self
            .http
            .post(self.search_url())
            .json(&body)
            .send()
            .await
            .map_err(|e| RepoError::Backend(format!("vespa search send: {e}")))?;
        if let Some(err) = map_status(resp.status(), "search") {
            return Err(err);
        }
        let v: Value = resp
            .json()
            .await
            .map_err(|e| RepoError::Backend(format!("vespa search decode: {e}")))?;
        Ok(PagedResult {
            items: parse_hits(&v),
            next_token: None,
        })
    }

    async fn index(&self, doc: IndexDoc) -> RepoResult<()> {
        let doc_type = sanitize_doc_type(&doc.type_id.0);
        let url = self.doc_url(&doc_type, &doc.tenant, &doc.id);
        let mut fields = match doc.payload.clone() {
            Value::Object(map) => Value::Object(map),
            other => json!({ "payload": other }),
        };
        if let Some(map) = fields.as_object_mut() {
            map.insert("id".into(), json!(doc.id.0));
            map.insert("tenant".into(), json!(doc.tenant.0));
            map.insert("type_id".into(), json!(doc.type_id.0));
            map.insert("version".into(), json!(doc.version));
            if let Some(emb) = &doc.embedding {
                map.insert("embedding".into(), json!({ "values": emb }));
            }
        }
        let body = json!({ "fields": fields });
        let condition = format!("{}.version < {}", doc_type, doc.version);
        let resp = self
            .http
            .put(url)
            .query(&[("condition", condition.as_str())])
            .json(&body)
            .send()
            .await
            .map_err(|e| RepoError::Backend(format!("vespa index send: {e}")))?;
        if resp.status().as_u16() == 412 {
            return Ok(());
        }
        if let Some(err) = map_status(resp.status(), "index") {
            return Err(err);
        }
        Ok(())
    }

    async fn delete(&self, tenant: &TenantId, id: &ObjectId) -> RepoResult<bool> {
        // The trait does not expose `type_id` at delete-time, so we
        // resolve it via a lookup search first.
        let q = SearchQuery {
            tenant: tenant.clone(),
            type_id: None,
            q: None,
            filters: [("id".to_string(), id.0.clone())].into_iter().collect(),
            page: Page { size: 1, token: None },
        };
        let hits = self.search(q, ReadConsistency::Eventual).await?;
        let Some(hit) = hits.items.into_iter().next() else {
            return Ok(false);
        };
        let doc_type = sanitize_doc_type(&hit.type_id.0);
        let url = self.doc_url(&doc_type, tenant, id);
        let resp = self
            .http
            .delete(url)
            .send()
            .await
            .map_err(|e| RepoError::Backend(format!("vespa delete send: {e}")))?;
        if resp.status().as_u16() == 404 {
            return Ok(false);
        }
        if let Some(err) = map_status(resp.status(), "delete") {
            return Err(err);
        }
        Ok(true)
    }

    async fn search_vector(
        &self,
        query: VectorQuery,
        _consistency: ReadConsistency,
    ) -> RepoResult<Vec<SearchHit>> {
        let mut where_clauses: Vec<String> = vec![
            format!("{{targetHits:{k}}}nearestNeighbor(embedding, q)", k = query.k.max(1)),
            format!("tenant contains \"{}\"", yql_escape(&query.tenant.0)),
        ];
        if let Some(t) = &query.type_id {
            where_clauses.push(format!("type_id contains \"{}\"", yql_escape(&t.0)));
        }
        for (k, v) in &query.filters {
            where_clauses.push(format!(
                "{} contains \"{}\"",
                sanitize_field(k),
                yql_escape(v)
            ));
        }
        let source = query
            .type_id
            .as_ref()
            .map(|t| sanitize_doc_type(&t.0))
            .unwrap_or_else(|| "*".into());
        let yql = format!(
            "select * from sources {} where {} limit {}",
            source,
            where_clauses.join(" and "),
            query.k.max(1)
        );
        let body = json!({
            "yql": yql,
            "input.query(q)": query.embedding,
            "ranking.profile": "embedding",
            "hits": query.k.max(1),
        });
        let resp = self
            .http
            .post(self.search_url())
            .json(&body)
            .send()
            .await
            .map_err(|e| RepoError::Backend(format!("vespa vector send: {e}")))?;
        if let Some(err) = map_status(resp.status(), "search_vector") {
            return Err(err);
        }
        let v: Value = resp
            .json()
            .await
            .map_err(|e| RepoError::Backend(format!("vespa vector decode: {e}")))?;
        Ok(parse_hits(&v))
    }

    async fn bulk_index(&self, docs: Vec<IndexDoc>) -> RepoResult<BulkOutcome> {
        // Vespa's `/document/v1` HTTP gateway is per-document; the
        // throughput knob is HTTP/2 multiplexing. We use the default
        // sequential loop here for correctness; the indexer service
        // can layer parallelism on top by sharding `docs` across
        // tasks.
        let mut out = BulkOutcome::default();
        for d in docs {
            let id = d.id.clone();
            match self.index(d).await {
                Ok(()) => out.indexed += 1,
                Err(e) => out.failed.push((id, e.to_string())),
            }
        }
        Ok(out)
    }
}

fn parse_hits(v: &Value) -> Vec<SearchHit> {
    let Some(children) = v
        .get("root")
        .and_then(|r| r.get("children"))
        .and_then(|c| c.as_array())
    else {
        return Vec::new();
    };
    children
        .iter()
        .filter_map(|h| {
            let fields = h.get("fields")?;
            let id = fields.get("id").and_then(|x| x.as_str())?.to_string();
            let type_id = fields
                .get("type_id")
                .and_then(|x| x.as_str())
                .unwrap_or("")
                .to_string();
            let score = h.get("relevance").and_then(|x| x.as_f64()).unwrap_or(0.0) as f32;
            Some(SearchHit {
                id: ObjectId(id),
                type_id: TypeId(type_id),
                score,
                snippet: Some(fields.clone()),
            })
        })
        .collect()
}
