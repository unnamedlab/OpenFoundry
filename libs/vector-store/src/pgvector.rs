use async_trait::async_trait;
use serde_json::Value;
use sqlx::{postgres::PgPoolOptions, PgPool, Row};

use crate::backend::{
    BackendConfig, Cursor, EmbeddingRecord, Hit, HybridQuery, VectorBackend, VectorBackendError,
};

/// pgvector-backed implementation using PostgreSQL + tsvector for BM25-style ranking.
pub struct PgvectorBackend {
    pool: PgPool,
}

impl PgvectorBackend {
    pub async fn new(config: &BackendConfig) -> Result<Self, VectorBackendError> {
        let url = config
            .database_url
            .as_deref()
            .ok_or_else(|| {
                VectorBackendError::Config(
                    "database_url required for pgvector backend".into(),
                )
            })?;
        let pool = PgPoolOptions::new()
            .max_connections(5)
            .connect(url)
            .await?;
        // Ensure the pgvector extension and table exist.
        sqlx::query(
            r#"
            CREATE EXTENSION IF NOT EXISTS vector;
            CREATE TABLE IF NOT EXISTS vector_embeddings (
                id          TEXT PRIMARY KEY,
                tenant_id   TEXT NOT NULL,
                namespace   TEXT NOT NULL,
                doc_id      TEXT NOT NULL,
                vector      vector,
                payload     JSONB NOT NULL DEFAULT '{}',
                ts          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
                fts         TSVECTOR GENERATED ALWAYS AS (to_tsvector('english', payload::text)) STORED,
                UNIQUE (tenant_id, namespace, doc_id)
            );
            CREATE INDEX IF NOT EXISTS vector_embeddings_tenant_ns
                ON vector_embeddings (tenant_id, namespace);
            CREATE INDEX IF NOT EXISTS vector_embeddings_fts
                ON vector_embeddings USING GIN (fts);
            "#,
        )
        .execute(&pool)
        .await?;
        Ok(Self { pool })
    }

    fn make_id(tenant_id: &str, namespace: &str, doc_id: &str) -> String {
        format!("{tenant_id}::{namespace}::{doc_id}")
    }
}

#[async_trait]
impl VectorBackend for PgvectorBackend {
    async fn upsert(&self, record: EmbeddingRecord) -> Result<(), VectorBackendError> {
        let id = Self::make_id(&record.tenant_id, &record.namespace, &record.doc_id);
        let vector_str = format!(
            "[{}]",
            record
                .vector
                .iter()
                .map(|v| v.to_string())
                .collect::<Vec<_>>()
                .join(",")
        );
        sqlx::query(
            r#"
            INSERT INTO vector_embeddings (id, tenant_id, namespace, doc_id, vector, payload, ts)
            VALUES ($1, $2, $3, $4, $5::vector, $6, $7)
            ON CONFLICT (tenant_id, namespace, doc_id)
            DO UPDATE SET vector = EXCLUDED.vector, payload = EXCLUDED.payload, ts = EXCLUDED.ts
            "#,
        )
        .bind(&id)
        .bind(&record.tenant_id)
        .bind(&record.namespace)
        .bind(&record.doc_id)
        .bind(&vector_str)
        .bind(&record.payload)
        .bind(record.ts)
        .execute(&self.pool)
        .await?;
        Ok(())
    }

    async fn delete(
        &self,
        tenant_id: &str,
        namespace: &str,
        doc_id: &str,
    ) -> Result<(), VectorBackendError> {
        sqlx::query(
            "DELETE FROM vector_embeddings WHERE tenant_id = $1 AND namespace = $2 AND doc_id = $3",
        )
        .bind(tenant_id)
        .bind(namespace)
        .bind(doc_id)
        .execute(&self.pool)
        .await?;
        Ok(())
    }

    async fn hybrid_query(&self, query: HybridQuery) -> Result<Vec<Hit>, VectorBackendError> {
        let vector_str = format!(
            "[{}]",
            query
                .vector
                .iter()
                .map(|v| v.to_string())
                .collect::<Vec<_>>()
                .join(",")
        );
        let keyword = query.keyword.as_deref().unwrap_or("");

        // Hybrid: vector cosine similarity + optional BM25-style ts_rank, fused with RRF.
        let rows = if keyword.is_empty() {
            sqlx::query(
                r#"
                SELECT doc_id, payload,
                       (1 - (vector <=> $3::vector)) AS score
                FROM vector_embeddings
                WHERE tenant_id = $1 AND namespace = $2
                ORDER BY vector <=> $3::vector
                LIMIT $4
                "#,
            )
            .bind(&query.tenant_id)
            .bind(&query.namespace)
            .bind(&vector_str)
            .bind(query.top_k as i64)
            .fetch_all(&self.pool)
            .await?
        } else {
            sqlx::query(
                r#"
                WITH vec_ranked AS (
                    SELECT doc_id, payload,
                           ROW_NUMBER() OVER (ORDER BY vector <=> $3::vector) AS vec_rank
                    FROM vector_embeddings
                    WHERE tenant_id = $1 AND namespace = $2
                ),
                text_ranked AS (
                    SELECT doc_id,
                           ROW_NUMBER() OVER (ORDER BY ts_rank(fts, plainto_tsquery('english', $5)) DESC) AS txt_rank
                    FROM vector_embeddings
                    WHERE tenant_id = $1 AND namespace = $2
                      AND fts @@ plainto_tsquery('english', $5)
                ),
                fused AS (
                    SELECT v.doc_id, v.payload,
                           (1.0 / (60 + v.vec_rank) + COALESCE(1.0 / (60 + t.txt_rank), 0)) AS rrf_score
                    FROM vec_ranked v
                    LEFT JOIN text_ranked t ON v.doc_id = t.doc_id
                )
                SELECT doc_id, payload, rrf_score AS score
                FROM fused
                ORDER BY rrf_score DESC
                LIMIT $4
                "#,
            )
            .bind(&query.tenant_id)
            .bind(&query.namespace)
            .bind(&vector_str)
            .bind(query.top_k as i64)
            .bind(keyword)
            .fetch_all(&self.pool)
            .await?
        };

        let hits = rows
            .into_iter()
            .filter_map(|row| {
                let score: f64 = row.try_get("score").ok()?;
                let score = score as f32;
                if score < query.min_score {
                    return None;
                }
                Some(Hit {
                    doc_id: row.try_get("doc_id").ok()?,
                    score,
                    payload: row.try_get::<Value, _>("payload").ok()?,
                })
            })
            .collect();

        Ok(hits)
    }

    async fn health(&self) -> Result<(), VectorBackendError> {
        sqlx::query("SELECT 1").execute(&self.pool).await?;
        Ok(())
    }

    async fn iter_embeddings(
        &self,
        tenant_id: &str,
        namespace: &str,
        cursor: Option<Cursor>,
        batch_size: usize,
    ) -> Result<(Vec<EmbeddingRecord>, Option<Cursor>), VectorBackendError> {
        let after_id = cursor.as_ref().map(|c| c.0.clone()).unwrap_or_default();

        let rows = sqlx::query(
            r#"
            SELECT id, tenant_id, namespace, doc_id, vector::text, payload, ts
            FROM vector_embeddings
            WHERE tenant_id = $1 AND namespace = $2
              AND ($3 = '' OR id > $3)
            ORDER BY id
            LIMIT $4
            "#,
        )
        .bind(tenant_id)
        .bind(namespace)
        .bind(&after_id)
        .bind(batch_size as i64 + 1)
        .fetch_all(&self.pool)
        .await?;

        let has_more = rows.len() > batch_size;
        let records: Vec<EmbeddingRecord> = rows
            .into_iter()
            .take(batch_size)
            .filter_map(|row| {
                let vector_text: String = row.try_get("vector").ok()?;
                let vector = parse_pg_vector(&vector_text)?;
                Some(EmbeddingRecord {
                    tenant_id: row.try_get("tenant_id").ok()?,
                    namespace: row.try_get("namespace").ok()?,
                    doc_id: row.try_get("doc_id").ok()?,
                    vector,
                    payload: row.try_get::<Value, _>("payload").ok()?,
                    ts: row.try_get("ts").ok()?,
                })
            })
            .collect();

        let next_cursor = if has_more {
            records
                .last()
                .map(|r| Cursor(Self::make_id(&r.tenant_id, &r.namespace, &r.doc_id)))
        } else {
            None
        };

        Ok((records, next_cursor))
    }

    async fn from_config(
        config: &BackendConfig,
    ) -> Result<Box<dyn VectorBackend>, VectorBackendError>
    where
        Self: Sized,
    {
        Ok(Box::new(Self::new(config).await?))
    }
}

fn parse_pg_vector(s: &str) -> Option<Vec<f32>> {
    let inner = s.trim().trim_start_matches('[').trim_end_matches(']');
    inner
        .split(',')
        .map(|v| v.trim().parse::<f32>().ok())
        .collect()
}
