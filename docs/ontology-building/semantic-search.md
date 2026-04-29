# Semantic search

Semantic search is the part of the ontology stack that helps users retrieve meaning, not just keywords.

In OpenFoundry, all hot-path search is served from **read-model projections** maintained by `ontology-query-service`. No search or KNN request scans the transactional `object_instances` table at serving time.

## Architecture of the search plane

Search results are served from two projection tables owned by `ontology-query-service`:

| Projection | What it stores |
|---|---|
| `query.search_document` | One row per object / type / interface / link / action / function / object-set: normalised `tsvector` for lexical search, an embedding vector for semantic recall, routing metadata, and security fields for pushdown filtering. GIN-indexed on `tsvector`. |
| `query.knn_vectors` | Per-property embedding vectors (`object_id`, `object_type_id`, `property_name`, `embedding`) with a pgvector HNSW or IVFFlat index for approximate nearest-neighbour lookup. |

Both projections are maintained by NATS JetStream consumers that process events emitted by `object-database-service` after every mutation. Projection freshness is near-real-time (milliseconds to low seconds).

Security visibility is applied as a **pushdown filter** from `query.policy_visibility` (compiled policy bundles), never by fetching objects and then filtering afterwards.

## Hybrid search path

`ontology-query-service` endpoint: `POST /api/v1/ontology/search`

Combines:

1. Full-text scoring (`tsvector` + GIN index on `query.search_document`)
2. Semantic candidate recall (embedding column on `query.search_document`, provider-backed or deterministic-hash fallback)
3. Provider-backed semantic reranking (when `SEARCH_EMBEDDING_PROVIDER` is configured)
4. Fusion strategies: `rrf` (reciprocal rank fusion) or `weighted`
5. Title bonus logic
6. Policy pushdown from `query.policy_visibility`

This is a real hybrid retrieval path, not simple keyword matching plus a local similarity hint. The entire path runs against projections; the transactional store is not consulted.

## KNN on ontology object types

`ontology-query-service` endpoint: `POST /api/v1/ontology/types/{type_id}/objects/knn`

Object properties declared with property type `vector` store numeric arrays as embeddings. KNN queries run against `query.knn_vectors`, not against `object_instances`.

Query parameters:

- `query_vector` or `anchor_object_id`
- `metric`: `cosine`, `dot_product`, or `euclidean`
- `limit` and optional property filter

This surface is exposed to callers beyond raw HTTP:

- the web client: `knnObjects(...)`
- the TypeScript SDK: `sdk.ontology.knnObjects(...)`
- the Python SDK: `sdk.ontology.knn_objects(...)`

## Knowledge-base retrieval in `ai-service`

`ai-service` maintains a separate RAG-oriented retrieval path via `knowledge-index-service` and `retrieval-context-service`:

- knowledge base creation and document ingestion
- embedding provider selection and chunk indexing
- embedding-based retrieval

This path is document-oriented and distinct from ontology-object search. The two surfaces are intentionally separate: `ontology-query-service` owns semantic retrieval over governed ontology objects; `ai-service` owns document-oriented embedding workflows.

## End-to-end semantic workflow

```
ingest (funnel / actions)
  └─► object-database-service (writes current state + outbox)
        └─► NATS JetStream
              ├─► query.search_document projection consumer
              │     └─► embed → store (tsvector + vector)
              └─► query.knn_vectors projection consumer
                    └─► store embedding by property

search request
  └─► ontology-query-service
        ├─ policy pushdown (query.policy_visibility)
        ├─ lexical recall (tsvector GIN on query.search_document)
        ├─ semantic recall (pgvector on query.search_document)
        ├─ reranking + fusion
        └─ return ranked results
```

## Design rules

1. Search and KNN endpoints are always "hot" — they must read from projections, never from `object_instances`.
2. Security is always applied as a pushdown filter compiled from policy bundles, before any result is returned.
3. Hybrid ranking (lexical + semantic) is the default; neither pure lexical nor pure vector-only is acceptable for the general search surface.
4. The same retrieval logic is exposed to functions, apps, and agents so the platform has one search plane.
5. Chunking and document ingestion for AI workflows is owned by `ai-service`, not by the ontology search path.

## Related pages

- [Object sets and search](/ontology-building/object-sets-and-search)
- [Functions](/ontology-building/functions)
- [Ontology-aware applications](/ontology-building/ontology-aware-applications)
- [Ontology architecture](/ontology-building/ontology-architecture/)
