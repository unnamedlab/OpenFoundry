# cassandra-kernel

Shared Cassandra (CQL) client kernel for OpenFoundry services. Built
on the official ScyllaDB Rust driver
([`scylla`](https://docs.rs/scylla)), which is currently the most
mature Rust driver for both ScyllaDB and Apache Cassandra: it speaks
native CQL, supports token-aware routing, prepared-statement caching,
paged streaming and DC-local load balancing.

ADR-0020 picks `scylla` as the only allowed Cassandra driver in the
codebase, and this crate is the only place where it is constructed
directly.

## What lives here

| Module    | Purpose                                                                  |
|-----------|--------------------------------------------------------------------------|
| `session` | [`ClusterConfig`] + [`SessionBuilder`] with the OpenFoundry defaults     |
| `shared`  | [`SharedSession`] — process-wide `Arc<Session>` holder                   |
| `query`   | Helpers: `paged_query`, `lwt_insert_if_not_exists`, `batch_logged`, `PreparedCache` |
| `migrate` | Versioned, idempotent CQL migrations (`Migration`, `apply`, `cql_migrate!`) |
| `error`   | `KernelError` / `KernelResult`                                           |

## Defaults applied by `SessionBuilder`

* **Local datacenter routing** via `DefaultPolicy::prefer_datacenter`.
  Cross-DC failover is **disabled**: a service pinned to `dc1` will
  not silently start serving from `dc2` if its local replicas go
  down. That is a paging condition, not a transparent fallback.
* **`LOCAL_QUORUM`** as default consistency for both reads and writes.
* **Token-aware** load balancing.
* **Default retry policy** with idempotency awareness.
* **Speculative execution**: 2 retries, 50 ms threshold.
* **Tracing opt-in**: `ClusterConfig::enable_tracing = true` only;
  off by default because per-request tracing materially loads
  `system_traces`.

## Usage

```rust,ignore
use cassandra_kernel::{cql_migrate, ClusterConfig, SharedSession};

static SESSION: SharedSession = SharedSession::new();

pub async fn boot() -> anyhow::Result<()> {
    let cfg = ClusterConfig {
        contact_points: vec![
            "of-cass-prod-dc1-service.cassandra.svc:9042".into(),
        ],
        local_datacenter: "dc1".into(),
        username: Some(std::env::var("CASS_USER")?),
        password: Some(std::env::var("CASS_PASS")?),
        keyspace: Some("ontology_objects".into()),
        ..ClusterConfig::dev_local()
    };
    let session = SESSION.init(cfg).await?;

    let migrations = cql_migrate![
        1, "create_objects" => &[
            "CREATE TABLE IF NOT EXISTS ontology_objects.objects ( \
                tenant_id text, object_id timeuuid, payload blob, \
                PRIMARY KEY ((tenant_id), object_id) \
             ) WITH CLUSTERING ORDER BY (object_id DESC)",
        ],
    ];
    cassandra_kernel::migrate::apply(&session, "ontology_objects", migrations).await?;
    Ok(())
}
```

### Reading with paging

```rust,ignore
use cassandra_kernel::query::{paged_query, PreparedCache};

let cache = PreparedCache::new();
let stmt = cache.get(&session, "SELECT object_id, kind FROM ontology_objects.objects WHERE tenant_id = ?").await?;
let mut iter = paged_query::<(uuid::Uuid, String)>(&session, &stmt, ("tenant-1",)).await?;
while let Some(row) = iter.next().await {
    let (id, kind) = row?;
    // ...
}
```

### LWT (Lightweight Transaction)

```rust,ignore
use cassandra_kernel::query::lwt_insert_if_not_exists;

// 4× the cost of a regular write — only when atomicity matters.
let applied = lwt_insert_if_not_exists(&session, &prepared, (idempotency_key, body)).await?;
if !applied {
    return Err(anyhow::anyhow!("duplicate request"));
}
```

### LOGGED batch

```rust,ignore
use cassandra_kernel::query::batch_logged;

// Single-partition only. Cross-partition LOGGED batches are a
// hotspot generator and ADR-0020 forbids them.
let token = compute_token("tenant-1");
let result = batch_logged(&session, vec![stmt_a, stmt_b], vec![vals_a, vals_b], token).await?;
```

## Migrations

Migrations are append-only, versioned and idempotent. Each migration
must use `IF NOT EXISTS` / `IF EXISTS` and must not write data —
schema only. Once a migration version is applied to any environment,
its source is frozen; the kernel verifies the checksum on every boot
and refuses to start with `KernelError::MigrationDrift` if the source
has been edited.

The ledger is a single per-keyspace table:

```cql
CREATE TABLE <ks>.cassandra_kernel_migrations (
    version    int,
    name       text,
    applied_at timestamp,
    checksum   text,
    PRIMARY KEY (version)
);
```

Cassandra has no native rollback. To "undo" a migration, write a new
forward migration that reverses the change.

## Tests

* Unit tests live next to each module.
* The integration test in `tests/integration.rs` spins up a real
  `cassandra:5.0.2` container via `testcontainers` and exercises the
  migration runtime end to end. It is `#[ignore]` by default because
  CI runners without docker would fail; run it locally with:

  ```bash
  cargo test -p cassandra-kernel -- --ignored
  ```

## Related

* ADR-0020 — Cassandra as operational store + 12 modelling rules.
* ADR-0021 — Temporal on Cassandra (Go workers, Rust client).
* `docs/architecture/data-model-cassandra.md` — full keyspace and
  table reference.
* `infra/k8s/platform/manifests/cassandra/` — operator, cluster CRs, dashboards, runbook.
