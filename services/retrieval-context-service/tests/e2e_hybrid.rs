// Run with: cargo test -p retrieval-context-service --features e2e -- e2e_hybrid
// Requires Docker to be available in the environment.

#[cfg(feature = "e2e")]
mod e2e_hybrid {
    use chrono::Utc;
    use serde_json::json;
    use testcontainers::{
        GenericImage, ImageExt,
        core::{IntoContainerPort, WaitFor},
        runners::AsyncRunner,
    };
    use vector_store::{BackendConfig, BackendKind, EmbeddingRecord, HybridQuery, build_backend};

    async fn spin_up_postgres() -> (testcontainers::ContainerAsync<GenericImage>, String) {
        let container = GenericImage::new("postgres", "16-alpine")
            .with_exposed_port(5432.tcp())
            .with_wait_for(WaitFor::message_on_stderr("database system is ready to accept connections"))
            .with_env_var("POSTGRES_PASSWORD", "postgres")
            .with_env_var("POSTGRES_USER", "postgres")
            .with_env_var("POSTGRES_DB", "postgres")
            .start()
            .await
            .expect("failed to start postgres container");

        let port = container.get_host_port_ipv4(5432u16).await.unwrap();
        let url = format!("postgres://postgres:postgres@127.0.0.1:{port}/postgres");
        (container, url)
    }

    /// Deterministic unit vector derived from doc index (one-hot in the given dimension).
    fn make_vector(doc_index: usize, dim: usize) -> Vec<f32> {
        let mut v = vec![0.0f32; dim];
        v[doc_index % dim] = 1.0;
        v
    }

    #[tokio::test]
    async fn test_pgvector_hybrid_search() {
        let (_container, db_url) = spin_up_postgres().await;

        let config = BackendConfig {
            kind: BackendKind::Pgvector,
            database_url: Some(db_url),
            vespa_url: None,
            dim: 4,
        };
        let backend = build_backend(&config).await.expect("failed to build backend");

        // Insert 10 docs with deterministic vectors.
        for i in 0..10usize {
            let record = EmbeddingRecord {
                tenant_id: "tenant-test".to_string(),
                namespace: "test-ns".to_string(),
                doc_id: format!("doc-{i:02}"),
                vector: make_vector(i, 4),
                payload: json!({ "text": format!("document number {i}"), "index": i }),
                ts: Utc::now(),
            };
            backend.upsert(record).await.expect("upsert failed");
        }

        // Query with a vector most similar to doc-02 (index=2 → v[2]=1.0).
        let query = HybridQuery {
            tenant_id: "tenant-test".to_string(),
            namespace: "test-ns".to_string(),
            vector: make_vector(2, 4),
            keyword: None,
            filter: None,
            top_k: 3,
            min_score: 0.0,
        };
        let hits = backend.hybrid_query(query).await.expect("hybrid_query failed");

        assert!(!hits.is_empty(), "expected at least one hit");
        assert_eq!(hits[0].doc_id, "doc-02", "expected doc-02 as top hit");
    }

    #[tokio::test]
    async fn test_pgvector_iter_embeddings() {
        let (_container, db_url) = spin_up_postgres().await;

        let config = BackendConfig {
            kind: BackendKind::Pgvector,
            database_url: Some(db_url),
            vespa_url: None,
            dim: 4,
        };
        let backend = build_backend(&config).await.expect("failed to build backend");

        // Insert 8 docs.
        for i in 0..8usize {
            let record = EmbeddingRecord {
                tenant_id: "tenant-iter".to_string(),
                namespace: "iter-ns".to_string(),
                doc_id: format!("doc-{i:02}"),
                vector: make_vector(i, 4),
                payload: json!({ "idx": i }),
                ts: Utc::now(),
            };
            backend.upsert(record).await.expect("upsert failed");
        }

        // Paginate with batch_size=3 and count all returned records.
        let mut total = 0;
        let mut cursor = None;
        loop {
            let (records, next) = backend
                .iter_embeddings("tenant-iter", "iter-ns", cursor, 3)
                .await
                .expect("iter_embeddings failed");
            total += records.len();
            cursor = next;
            if cursor.is_none() {
                break;
            }
        }
        assert_eq!(total, 8, "expected 8 total records from iteration");
    }
}
