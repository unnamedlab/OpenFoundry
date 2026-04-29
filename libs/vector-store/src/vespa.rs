use async_trait::async_trait;
use serde_json::{json, Value};

use crate::backend::{
    BackendConfig, Cursor, EmbeddingRecord, Hit, HybridQuery, VectorBackend, VectorBackendError,
};

/// Vespa-backed implementation using Document API + Search API.
pub struct VespaBackend {
    client: reqwest::Client,
    base_url: String,
    #[allow(dead_code)]
    dim: usize,
}

impl VespaBackend {
    pub fn new(config: &BackendConfig) -> Result<Self, VectorBackendError> {
        let base_url = config
            .vespa_url
            .as_deref()
            .ok_or_else(|| {
                VectorBackendError::Config("vespa_url required for vespa backend".into())
            })?
            .trim_end_matches('/')
            .to_string();
        Ok(Self {
            client: reqwest::Client::new(),
            base_url,
            dim: config.dim,
        })
    }

    fn doc_url(&self, tenant_id: &str, namespace: &str, doc_id: &str) -> String {
        let encoded_id = format!("{tenant_id}::{namespace}::{doc_id}");
        format!(
            "{}/document/v1/openfoundry/embedding/docid/{}",
            self.base_url,
            urlencoding::encode(&encoded_id)
        )
    }
}

#[async_trait]
impl VectorBackend for VespaBackend {
    async fn upsert(&self, record: EmbeddingRecord) -> Result<(), VectorBackendError> {
        let url = self.doc_url(&record.tenant_id, &record.namespace, &record.doc_id);
        let body = json!({
            "fields": {
                "tenant_id": record.tenant_id,
                "namespace": record.namespace,
                "doc_id": record.doc_id,
                "embedding": { "values": record.vector },
                "payload": record.payload.to_string(),
                "ts": record.ts.timestamp_millis(),
            }
        });
        let response = self.client.put(&url).json(&body).send().await?;
        if !response.status().is_success() {
            let status = response.status();
            let text = response.text().await.unwrap_or_default();
            return Err(VectorBackendError::Unavailable(format!(
                "Vespa upsert failed {status}: {text}"
            )));
        }
        Ok(())
    }

    async fn delete(
        &self,
        tenant_id: &str,
        namespace: &str,
        doc_id: &str,
    ) -> Result<(), VectorBackendError> {
        let url = self.doc_url(tenant_id, namespace, doc_id);
        let response = self.client.delete(&url).send().await?;
        if !response.status().is_success() && response.status().as_u16() != 404 {
            let status = response.status();
            let text = response.text().await.unwrap_or_default();
            return Err(VectorBackendError::Unavailable(format!(
                "Vespa delete failed {status}: {text}"
            )));
        }
        Ok(())
    }

    async fn hybrid_query(&self, query: HybridQuery) -> Result<Vec<Hit>, VectorBackendError> {
        let vector_values = query.vector.iter().map(|v| *v as f64).collect::<Vec<_>>();

        let yql = if query.keyword.is_some() {
            format!(
                r#"select * from sources embedding where ({{targetHits:{top_k}}}nearestNeighbor(embedding, q_emb)) and tenant_id contains "{tenant}" and namespace contains "{ns}" and userQuery() limit {top_k}"#,
                top_k = query.top_k,
                tenant = query.tenant_id,
                ns = query.namespace,
            )
        } else {
            format!(
                r#"select * from sources embedding where ({{targetHits:{top_k}}}nearestNeighbor(embedding, q_emb)) and tenant_id contains "{tenant}" and namespace contains "{ns}" limit {top_k}"#,
                top_k = query.top_k,
                tenant = query.tenant_id,
                ns = query.namespace,
            )
        };

        let mut request_body = json!({
            "yql": yql,
            "input.query(q_emb)": { "values": vector_values },
            "ranking.profile": "hybrid",
            "hits": query.top_k,
        });

        if let Some(keyword) = &query.keyword {
            request_body["query"] = json!(keyword);
        }

        let url = format!("{}/search/", self.base_url);
        let response = self.client.post(&url).json(&request_body).send().await?;
        if !response.status().is_success() {
            let status = response.status();
            let text = response.text().await.unwrap_or_default();
            return Err(VectorBackendError::Unavailable(format!(
                "Vespa search failed {status}: {text}"
            )));
        }

        let result: Value = response.json().await?;
        let hits = result["root"]["children"]
            .as_array()
            .map(|children| {
                children
                    .iter()
                    .filter_map(|child| {
                        let doc_id = child["fields"]["doc_id"].as_str()?.to_string();
                        let score = child["relevance"].as_f64()? as f32;
                        if score < query.min_score {
                            return None;
                        }
                        let payload_str = child["fields"]["payload"].as_str().unwrap_or("{}");
                        let payload: Value =
                            serde_json::from_str(payload_str).unwrap_or(json!({}));
                        Some(Hit {
                            doc_id,
                            score,
                            payload,
                        })
                    })
                    .collect::<Vec<_>>()
            })
            .unwrap_or_default();

        Ok(hits)
    }

    async fn health(&self) -> Result<(), VectorBackendError> {
        let url = format!("{}/ApplicationStatus", self.base_url);
        let response = self.client.get(&url).send().await?;
        if !response.status().is_success() {
            return Err(VectorBackendError::Unavailable(
                "Vespa health check failed".into(),
            ));
        }
        Ok(())
    }

    async fn iter_embeddings(
        &self,
        tenant_id: &str,
        namespace: &str,
        cursor: Option<Cursor>,
        batch_size: usize,
    ) -> Result<(Vec<EmbeddingRecord>, Option<Cursor>), VectorBackendError> {
        let mut url = format!(
            "{}/document/v1/openfoundry/embedding/docid/?wantedDocumentCount={}&selection=embedding.tenant_id==\"{}\" and embedding.namespace==\"{}\"",
            self.base_url, batch_size + 1, tenant_id, namespace,
        );
        if let Some(c) = &cursor {
            url.push_str(&format!("&continuation={}", urlencoding::encode(&c.0)));
        }

        let response = self.client.get(&url).send().await?;
        if !response.status().is_success() {
            let status = response.status();
            let text = response.text().await.unwrap_or_default();
            return Err(VectorBackendError::Unavailable(format!(
                "Vespa visit failed {status}: {text}"
            )));
        }

        let result: Value = response.json().await?;
        let continuation = result["continuation"]
            .as_str()
            .map(|s| Cursor(s.to_string()));

        let records = result["documents"]
            .as_array()
            .map(|docs| {
                docs.iter()
                    .take(batch_size)
                    .filter_map(|doc| {
                        let fields = &doc["fields"];
                        let doc_id = fields["doc_id"].as_str()?.to_string();
                        let vector_values = fields["embedding"]["values"]
                            .as_array()?
                            .iter()
                            .filter_map(|v| v.as_f64().map(|f| f as f32))
                            .collect::<Vec<_>>();
                        let payload_str = fields["payload"].as_str().unwrap_or("{}");
                        let payload: Value =
                            serde_json::from_str(payload_str).unwrap_or(json!({}));
                        let ts = fields["ts"]
                            .as_i64()
                            .and_then(chrono::DateTime::from_timestamp_millis)
                            .unwrap_or_else(chrono::Utc::now);
                        Some(EmbeddingRecord {
                            tenant_id: tenant_id.to_string(),
                            namespace: namespace.to_string(),
                            doc_id,
                            vector: vector_values,
                            payload,
                            ts,
                        })
                    })
                    .collect::<Vec<_>>()
            })
            .unwrap_or_default();

        Ok((records, continuation))
    }

    async fn from_config(
        config: &BackendConfig,
    ) -> Result<Box<dyn VectorBackend>, VectorBackendError>
    where
        Self: Sized,
    {
        Ok(Box::new(Self::new(config)?))
    }
}
