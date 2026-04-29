# Ontology architecture

The ontology architecture is the part of OpenFoundry that turns many independent platform services into one operational system.

Without this layer, the repo is still a capable data and workflow platform. With this layer, it can become a governed semantic operating system where objects, relationships, policies, and actions form a queryable, auditable knowledge fabric.

## Target architecture: CQRS by cells

OpenFoundry converges to a **CQRS-by-cells** architecture with a separated control plane and data plane, where the hot serving path never depends on the transactional store or on a single central service.

### Guiding principles

- The hot read path (search, graph, KNN, object views) is always served from **read-model projections**, never by scanning transactional tables.
- Every mutation goes through `object-database-service`, which writes current state, an append-only revision, and a transactional outbox row atomically.
- The outbox feeds NATS JetStream; JetStream consumers maintain the read-model projections asynchronously.
- Security decisions are always **pushdown filters** compiled from policy bundles, never fetch-then-filter.
- No Qdrant or non-OSS components. Target infra: PostgreSQL + pgvector + Redis + NATS JetStream + Patroni/etcd/PgBouncer.

## Control plane and data plane

### Control plane

Owned exclusively by **`ontology-definition-service`** (port `50103`).

The control plane defines and governs:

- object types, properties, interfaces, shared property types, link types
- action type definitions
- function package registry and versions
- object-set definitions
- funnel source definitions
- projects, branches, proposals, migrations, and resource bindings

The definition service publishes **versioned schema and policy bundles** over NATS JetStream to every data-plane cell. The data plane never calls the control plane synchronously on the hot path; it operates from the last received bundle.

### Data plane

The data plane is subdivided into three sub-planes.

#### Write store

Owned exclusively by **`object-database-service`** (port `50104`).

Responsibilities:

- Single write authority for `object_instances`, `link_instances`.
- Maintains `object_revisions` and `link_revisions` as an append-only history for auditing, replay, and conflict resolution.
- Maintains `write_outbox` for transactional publication to NATS JetStream.
- Exposes idempotent upsert APIs with optimistic concurrency and get-by-id for consistent reads.

Every mutation is a single transaction: current state + revision + outbox row. No other service writes to these tables.

#### Serving plane (read models)

Owned exclusively by **`ontology-query-service`** (port `50105`).

The serving plane reads only from read-model projections maintained by JetStream consumers:

| Projection | Purpose |
|---|---|
| `query.object_current` | Denormalised current state per object (type, display title, normalised properties, org, marking, project, timestamps) |
| `query.link_adjacency` | Precomputed inbound/outbound adjacency indexed by source, target, and link type |
| `query.search_document` | One document per object/type/interface/link/action/function/object-set; `tsvector` + embedding + routing/security metadata; GIN-indexed |
| `query.knn_vectors` | Per-property embedding vectors with pgvector HNSW/IVFFlat index |
| `query.object_view` | UI-ready summary: applicable actions, neighbour counts, rule hints |
| `query.object_set_membership` | Incremental, pageable set membership (replaces `materialized_snapshot` JSONB) |
| `query.policy_visibility` | Compiled access expansion per workspace/project/marking/restricted view; used for query pushdown |
| `query.funnel_health` | Aggregated funnel run health; explicit read model, not ad-hoc aggregation |

Redis is the hot cache in front of `object_view` and `search_document` lookups. The query service never scans `object_instances` for hot paths.

Endpoints served exclusively from projections:

- `/search`, `/graph`, `/quiver`, `/object-sets`
- `/types/{id}/objects/query`, `/types/{id}/objects/knn`
- enriched object view, neighbours
- exploratory analysis and timeseries analytics feeds
- paginated operational lists

Endpoints that may still hit the transactional store (consistent reads):

- get object by ID, get link by ID
- write-path validations
- branch/proposal recovery

The optional `read-your-own-write` request token enables a per-request fallback to `object-database-service` for a single consistent read after a write.

#### Bounded collaborators

| Service | Port | Responsibility |
|---|---|---|
| `ontology-actions-service` | `50106` | Validate / plan / execute; coordinates workflow and notifications; delegates all mutations to `object-database-service`; emits `ActionPlanned` / `ActionExecuted` / `ActionFailed` |
| `ontology-funnel-service` | `50107` | Idempotent batch and streaming ingestion into `object-database-service`; funnel run health and backfill |
| `ontology-functions-service` | `50108` | Governed function-package runtime; reads via `ontology-query-service`; writes via `ontology-actions-service` / `object-database-service` only |
| `ontology-security-service` | `50109` | Compiles versioned policy bundles from governance rules; distributes bundles per cell; resolves clearances, markings, restricted views; serves decision cache |

## Cell topology

A **cell** is a deployment unit that contains a full data-plane stack:

- `edge-gateway-service` (router only, no business logic)
- `object-database-service` (write store, PostgreSQL HA local)
- `ontology-query-service` (serving, Redis HA local)
- `ontology-actions-service`
- `ontology-security-service`
- `ontology-funnel-service`
- `ontology-functions-service`
- PostgreSQL HA (Patroni + etcd + PgBouncer)
- Redis HA (Sentinel or Cluster)
- NATS JetStream cluster (3 nodes)

`ontology-definition-service` operates as a **regional/global control plane** outside the cell. It publishes versioned bundles to each cell over JetStream. Serving never requires a synchronous call to the control plane.

### Cell routing rules

- Each object and link has exactly one **home cell** (region + workspace shard).
- Searches are **cell-local first**.
- Cross-cell queries are **explicit federation fan-out** with strict timeouts and partial-result semantics — never a hot graph join.
- Cross-cell relations are **federated references**, not synchronous remote-graph dependencies.
- Each cell must degrade autonomously if another cell is unavailable.

### No-SPOF target

| Component | HA mechanism |
|---|---|
| PostgreSQL | Patroni + etcd + réplicas + PgBouncer |
| Redis | Sentinel or Cluster |
| NATS JetStream | 3-node cluster |
| Gateway and query | Minimum 3 replicas |

No policy decision critical to serving depends on a single central instance.

## Write path

```
funnel / actions / direct write
  └─► object-database-service
        ├─ writes: current state + revision + outbox (one transaction)
        └─ outbox relay → NATS JetStream
             └─► projection consumers → query.* tables
                   └─► ontology-query-service (serving)
```

## Read path

```
client request
  └─► edge-gateway-service (router)
        └─► ontology-query-service
              ├─ Redis hot cache
              ├─ PostgreSQL query.* projections (pgvector + FTS/GIN)
              └─ security pushdown from query.policy_visibility
```

Consistency: eventual by default (milliseconds to low seconds). `read-your-own-write` token available per request for immediate consistency on a single object.

## Evolution phases

| Phase | Goal |
|---|---|
| **Phase 0** | Align docs, protos, and routing with target ownership. No behaviour change. |
| **Phase 1** | Logical schema separation in one PostgreSQL; introduce outbox and JetStream; build `query.*` projections; switch hot reads to projections; compile policy bundles. |
| **Phase 2** | Physical separation into three clusters (control, object-write, query/read); add Redis HA, read replicas, pgvector tuning. |
| **Phase 3** | Operate by cells; control plane publishes bundles outward; federation fan-out for cross-cell queries. |

## Related pages

- [Indexing and materialization](/ontology-building/indexing-and-materialization)
- [Object edits and conflict resolution](/ontology-building/object-edits-and-conflict-resolution)
- [Action types](/ontology-building/action-types)
- [Functions](/ontology-building/functions)
- [Semantic search](/ontology-building/semantic-search)
