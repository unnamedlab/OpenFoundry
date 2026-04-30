# vector-store

Embedding generation and vector / hybrid search for OpenFoundry.

The crate exposes a small, backend-agnostic abstraction
(`vector_store::VectorBackend`) and one or more concrete backends gated
behind Cargo features:

| Backend  | Feature   | Status   | Use case                                     |
|----------|-----------|----------|----------------------------------------------|
| pgvector | (default) | skeleton | Existing PostgreSQL deployments              |
| Vespa    | `vespa`   | ready    | Hybrid BM25 + ANN HNSW + phased ranking      |

Enabling a backend is purely additive: turning on `vespa` does not
change the `pgvector` surface and vice-versa.

## Trait

```rust,ignore
#[async_trait]
pub trait VectorBackend: Send + Sync {
    async fn upsert(&self, doc_id: &str,
                    fields: &BTreeMap<String, serde_json::Value>,
                    embedding: &[f32]) -> BackendResult<()>;
    async fn delete(&self, doc_id: &str) -> BackendResult<()>;
    async fn hybrid_query(&self, text: &str, embedding: &[f32],
                          filter: &Filter, top_k: usize)
                          -> BackendResult<Vec<QueryHit>>;
}
```

`hybrid_query` is the single retrieval entry point: pass `text` for the
lexical (BM25) signal, `embedding` for the dense (ANN) signal, or both
for hybrid ranking. An empty string / empty slice disables that signal.

## Vespa backend

```toml
[dependencies]
vector-store = { workspace = true, features = ["vespa"] }
```

```rust,ignore
use std::collections::BTreeMap;
use serde_json::json;
use vector_store::{Filter, VectorBackend};
use vector_store::vespa::{VespaBackend, VespaConfig};

# async fn run() -> Result<(), Box<dyn std::error::Error>> {
let backend = VespaBackend::new(VespaConfig::new("http://localhost:8080"))?;

// Upsert a document with a 4-dim embedding.
let mut fields = BTreeMap::new();
fields.insert("text".into(), json!("the quick brown fox"));
fields.insert("tenant_id".into(), json!("acme"));
backend.upsert("doc-1", &fields, &[1.0, 0.0, 0.0, 0.0]).await?;

// Hybrid query: BM25(text) + ANN closeness(embedding), ranked by the
// `hybrid` rank-profile, restricted to a single tenant.
let hits = backend
    .hybrid_query("fox", &[1.0, 0.0, 0.0, 0.0],
                  &Filter::eq("tenant_id", "acme"),
                  10)
    .await?;
for h in hits {
    println!("{} score={}", h.id, h.score);
}
# Ok(()) }
```

### Vespa application package

The backend speaks plain HTTP/JSON against:

* `POST   /document/v1/<namespace>/<doctype>/docid/<id>` (upsert)
* `DELETE /document/v1/<namespace>/<doctype>/docid/<id>` (delete)
* `POST   /search/` (hybrid YQL query with an `input.query(q_embedding)`
  tensor parameter)

Default config maps to namespace `default`, document type `doc`, BM25
field `text`, tensor field `embedding`, rank profile `hybrid`. A working
minimal application package — used by the integration test — is shipped
under [`tests/fixtures/vespa-app/`](tests/fixtures/vespa-app/). The
schema looks like:

```text
schema doc {
  document doc {
    field text type string {
      indexing: index | summary
      index: enable-bm25
    }
    field tenant_id type string {
      indexing: attribute | summary
    }
    field embedding type tensor<float>(x[N]) {
      indexing: attribute | index
      attribute { distance-metric: angular }
      index {
        hnsw {
          max-links-per-node: 16
          neighbors-to-explore-at-insert: 200
        }
      }
    }
  }

  rank-profile hybrid {
    inputs { query(q_embedding) tensor<float>(x[N]) }
    first-phase  { expression: bm25(text) + closeness(field, embedding) }
    second-phase { expression: firstPhase   rerank-count: 50 }
  }
}
```

### HNSW tuning

The two HNSW knobs that matter most in practice:

* **`max-links-per-node` (M)** – out-degree of every graph node.
  * `8`–`16` for small or low-dimensional collections (cheaper memory,
    faster build).
  * `32`–`64` when recall matters more than memory and dimensions are
    high (≥ 768). Memory cost is roughly `M * 8 bytes * num_docs`.
* **`neighbors-to-explore-at-insert` (efConstruction)** – build-time
  beam width.
  * `100`–`200` is a good default. Increasing it improves recall at the
    cost of (one-shot) indexing time.

At query time, `targetHits` (set automatically by this backend to
`top_k`) acts as the search-time `ef`. If you need higher recall without
re-indexing, raise `top_k` (the rank-profile will discard extras after
re-ranking) or override the `ranking.profile` in the config to one that
uses a larger `targetHits` hint via a YQL annotation.

The `distance-metric` should match how your embeddings were trained:
`angular` for cosine-normalised vectors (most LLM embeddings),
`euclidean` for L2-trained models, `dotproduct` if you skip
normalisation.

### Phased ranking

The `hybrid` rank-profile uses Vespa's two-phase ranking:

* **first-phase** evaluates on every candidate that survives the WAND /
  HNSW match phase. Keep it cheap: `bm25(text) + closeness(field,
  embedding)` is usually enough.
* **second-phase** runs only on the top `rerank-count` documents per
  content node. This is where you plug a learned ranker
  (`onnx-model`-backed cross-encoder, GBDT, custom expression…). The
  fixture intentionally re-uses `firstPhase` so the test is
  deterministic — replace it in production.

## pgvector backend

The `pgvector` module currently exposes the trait surface only
(`PgVectorBackend::new()` returns a value whose methods all yield
`BackendError::Unimplemented`). This anchors the contract so consumers
can already program against `Box<dyn VectorBackend>`; the full
PostgreSQL/`pgvector` wiring will land in a follow-up without changing
the public surface.

## Testing

```bash
# Pure unit tests (no Docker required) — run on every PR:
cargo test -p vector-store --features vespa

# End-to-end test against a real Vespa container (requires Docker and
# pulls the multi-GB vespaengine/vespa image):
cargo test -p vector-store --features vespa \
    --test vespa_integration -- --ignored
```
