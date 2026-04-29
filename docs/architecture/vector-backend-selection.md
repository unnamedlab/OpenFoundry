# Vector Backend Selection

OpenFoundry supports two vector store backends: **pgvector** (PostgreSQL extension) and **Vespa**.
The `VectorBackendRouter` in `libs/vector-store` selects the correct backend per request based on tenant configuration.

## Selection Criteria

| Criterion | pgvector | Vespa |
|---|---|---|
| **Volume of embeddings** | < ~100k vectors/tenant | Millions of vectors |
| **Write throughput** | Low–medium (transactional) | High (distributed) |
| **Hybrid/BM25 search** | tsvector + ts_rank + RRF | WAND/BM25 natively |
| **Target latency** | < 100 ms p95 (small datasets) | < 20 ms p95 at scale |
| **Tenant isolation** | Row-level with indexes | Schema/cluster-level |
| **Operational cost** | Low (reuses existing PG) | Higher (dedicated cluster) |
| **Transactional coupling** | Yes — same Postgres instance | No |
| **Default environment** | **dev** | **prod** |

## Default Rules

| Environment | Backend |
|---|---|
| `dev` / `default.toml` | `pgvector` |
| `prod` / `prod.toml` | `vespa` |

**Override per tenant** when:
- The tenant's embeddings are tightly coupled to transactional data in Postgres (e.g., audit events, per-row document indexing), **and**
- The embedding volume is small (< ~100k vectors/tenant).

Environment variable override:
```bash
OPENFOUNDRY__TENANT__VECTOR_BACKEND=vespa
```

Per-tenant override in config:
```toml
[tenant.overrides.my-tenant-id]
vector_backend = "pgvector"
```

## VectorBackendRouter Resolution

```
Request (tenant_id) → AppState.vector_router
                              │
                    ┌─────────▼──────────┐
                    │ VectorBackendRouter │
                    │  for_tenant(id)     │
                    └──────┬─────────────┘
                           │
             ┌─────────────▼──────────────┐
             │   overrides.get(tenant_id)  │
             │   present? → use override   │
             │   absent?  → default backend│
             └─────────────────────────────┘
```

The `VectorBackendRouter` is constructed at startup:
1. Read `config.tenant.vector_backend` → build the **default** backend.
2. For each entry in `config.tenant.overrides`, build its backend and register it.
3. Backends are cached in `Arc<dyn VectorBackend>` — no reconnection per request.

## Migration Procedure (`of vector reindex`)

Use the `of vector reindex` subcommand to migrate embeddings between backends without downtime:

```bash
# 1. Dry-run: count + validate shape
of vector reindex \
  --from pgvector --from-url "postgres://..." \
  --to vespa --to-url "http://vespa:8080" \
  --tenant my-tenant --namespace docs \
  --dry-run

# 2. Reindex (with checkpoint for resumability)
of vector reindex \
  --from pgvector --from-url "postgres://..." \
  --to vespa --to-url "http://vespa:8080" \
  --tenant my-tenant --namespace docs \
  --batch-size 500 \
  --checkpoint-file reindex-my-tenant.json

# 3. Swap the per-tenant setting in config or environment
OPENFOUNDRY__TENANT__OVERRIDES__my-tenant__VECTOR_BACKEND=vespa

# 4. Verify
of vector reindex \
  --from vespa --from-url "http://vespa:8080" \
  --to vespa --to-url "http://vespa:8080" \
  --tenant my-tenant --namespace docs \
  --dry-run
```

Each batch emits a JSON line to stdout:
```json
{"batch":1,"records_in_batch":500,"total_records":500,"dry_run":false,"validation_errors":0,"cursor":"..."}
```

The `--checkpoint-file` persists the cursor after each confirmed batch so that interrupted runs can be resumed with `--checkpoint-file` pointing to the same file.

## Idempotency

`upsert` is keyed by `(tenant_id, namespace, doc_id)`. Re-running a reindex is safe — duplicate records are updated in place.

## See Also

- [`libs/vector-store/src/backend.rs`](../../libs/vector-store/src/backend.rs) — trait definition
- [`libs/vector-store/src/pgvector.rs`](../../libs/vector-store/src/pgvector.rs) — pgvector implementation
- [`libs/vector-store/src/vespa.rs`](../../libs/vector-store/src/vespa.rs) — Vespa implementation
- [`tools/of-cli/src/vector.rs`](../../tools/of-cli/src/vector.rs) — reindex CLI
