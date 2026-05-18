# ADR-0010: CloudNativePG (CNPG) as the sole PostgreSQL operator

- **Status:** Accepted
- **Date:** 2026-04-29
- **Deciders:** OpenFoundry platform architecture group
- **Related work:**
  - `docs/architecture/runtime-topology.md` — "Postgres for service-owned
    relational state" (database-per-service).
  - `services/*/internal/repo/migrations/` — each service maintains its own schema
    (e.g. `services/sql-bi-gateway-service/internal/repo/migrations`,
    `services/identity-federation-service/internal/repo/migrations`, etc.).
    The original example list (`cipher-service`, `data-asset-catalog-service`,
    `marketplace-service`) reflected the pre-consolidation service taxonomy;
    after [ADR-0030](./ADR-0030-service-consolidation-30-targets.md) those
    migrations live in the consolidated owners
    (`audit-compliance-service`, `iceberg-catalog-service` + `dataset-versioning-service`,
    `federation-product-exchange-service` respectively). The CNPG decision
    itself is not affected by which services exist — it scopes the operator
    used for every Postgres cluster on the platform.
  - `infra/helm/infra/manifests/cnpg/templates/cluster.yaml`
    — reference template for the `postgresql.cnpg.io/v1 Cluster` CRD
    used by platform services.
  - `infra/helm/infra/manifests/rook/` (`cluster.yaml`, `objectstore.yaml`, `bucket.yaml`) —
    Ceph + RGW as the cluster-local S3 provider.
  - ADR-0007 (`docs/architecture/adr/ADR-0007-search-engine-choice.md`) —
    precedent for "a single operator / a single stateful stack per capability".

## Context

OpenFoundry follows the **database-per-service** pattern: every bounded
context owns its own logical Postgres, with versioned migrations under
`services/<svc>/migrations/`. This is confirmed in
`docs/architecture/runtime-topology.md`:

> "Postgres for service-owned relational state […] The CI smoke job creates
> multiple service-specific databases, which strongly suggests
> database-per-service isolation rather than a shared operational schema."

Today two realities coexist:

1. **Isolated use of CloudNativePG** already introduced as a reference
   template for platform Postgres clusters (see
   `infra/helm/infra/manifests/cnpg/templates/cluster.yaml`),
   where the operator reconciles a cluster with streaming replication and
   exposes the `<name>-rw` / `<name>-ro` services.
2. **The remaining services** have no standardised operator. The
   historically considered alternatives (external Patroni + HAProxy/VIP,
   hand-rolled StatefulSets, vendor-managed Postgres) introduce:
   - additional VIPs and load balancers (operational SPOFs),
   - divergent failover paths per service,
   - bespoke backup/restore per team,
   - difficulty auditing RPO/RTO uniformly,
   - duplicated runbooks.

Without a single operator:

- Each team reinvents HA, backups, WAL archive, credential rotation, and
  minor/major upgrades.
- There is no standard way to declare topology (primary + replicas),
  maintenance windows, or retention policies.
- The cluster-local S3 storage plane available
  (`infra/helm/infra/manifests/rook/objectstore.yaml`) is under-used for
  WAL/base backups.

The project maintains a **100% OSS** stance (Apache-2.0 / MIT / BSD); the
chosen operator must fit that constraint and align with the precedent
set by ADR-0007 ("a single stateful stack per capability").

## Options considered

### Option A — CloudNativePG (CNPG) as the sole operator (chosen)

- Apache-2.0 operator, Kubernetes-native, with no dependency on external
  etcd or Patroni.
- `Cluster` CRD that declares topology (instances, sync replicas),
  bootstrap, storage, resources, scheduled backups, and
  `barmanObjectStore` for WAL/base backups to S3.
- Failover managed by the operator via the Kubernetes API (no VIP).
- `<name>-rw`, `<name>-ro`, `<name>-r` services generated automatically
  → application services connect via stable DNS.
- Already in use as a reference template under
  `infra/helm/infra/manifests/cnpg/` (live precedent in the repo).

### Option B — Patroni + HAProxy / keepalived (VIP)

- Requires external etcd/Consul, HAProxy, and a per-cluster VIP.
- Adds operational SPOFs (VIP, balancer), manual failover runbooks, and
  a control plane outside of Kubernetes.
- Backups and WAL archive resolved with ad-hoc scripts (pgBackRest/barman
  invoked manually).

### Option C — Zalando postgres-operator

- OSS (MIT), mature, based on Patroni internally.
- Configuration model (Spilo/Patroni) more opaque than CNPG and with a
  larger tuning surface.
- Backup integration via WAL-G; correct but less declarative than CNPG's
  `barmanObjectStore`.

### Option D — StackGres

- OSS (AGPL-3.0 for core components in some distributions).
- **Rejected** due to incompatibility with the project's OSS stance
  (Apache-2.0 / MIT / BSD), analogous to the reasoning applied in
  ADR-0007.

### Option E — Status quo (StatefulSets without an operator)

- Keep Postgres per service without an operator, with HA and backups
  managed by each team.
- Maximises operational debt and disperses runbooks; does not solve the
  problem at hand.

## Decision

We adopt **Option A — CloudNativePG (CNPG) as the sole PostgreSQL
operator** for every Postgres cluster in OpenFoundry's production plane.

- **Sole operator.** CNPG (Apache-2.0) is the only operator supported
  for Postgres in `infra/helm/**`. No external Patroni, VIPs, Postgres
  HAProxy, or alternative operators (Zalando, StackGres, etc.) will be
  introduced while this ADR is in force.
- **Topology per bounded context.** Every service that requires Postgres
  declares its own `postgresql.cnpg.io/v1 Cluster` CR with the base
  topology **1 primary + 2 replicas (at least 1 synchronous)**
  (`spec.instances: 3`, `spec.minSyncReplicas: 1`,
  `spec.maxSyncReplicas: 1`). This preserves the database-per-service
  isolation described in `docs/architecture/runtime-topology.md`.
- **Backups and WAL archive to Ceph RGW.** All clusters configure
  `spec.backup.barmanObjectStore` pointing to
  `s3://openfoundry-pg-backups/<service>/<cluster>/`, served by the
  Ceph RGW declared in
  `infra/helm/infra/manifests/rook/objectstore.yaml` and
  `infra/helm/infra/manifests/rook/bucket.yaml`. This includes:
  - Continuous **WAL archiving** (`wal: { compression: gzip }`).
  - **Scheduled base backups** (`ScheduledBackup`) on a daily cadence.
  - Minimum **retention** of 14 days, maximum of 30 days by default;
    overridable per service.
  - **Encryption at rest** delegated to the Ceph RGW bucket.
- **Service connections.** Services consume the endpoints generated by
  CNPG: `<cluster>-rw` for writes, `<cluster>-ro` for scalable reads.
  Direct connections to pods or external VIPs are not allowed.
- **Credentials.** Managed via a `Secret` of type
  `kubernetes.io/basic-auth`, following the pattern established in
  `infra/helm/infra/manifests/cnpg/templates/cluster.yaml`.
- **Upgrades.** Major Postgres versions are planned via the operator's
  in-place upgrade flow or via a logical-replica `Cluster` + cutover,
  as documented in the runbook.
- **Local development.** `infra/compose/docker-compose.yml` and
  `infra/compose/docker-compose.dev.yml` continue to use standard Postgres
  containers for DX. CNPG applies only to the Kubernetes plane.

## Consequences

### Positive

- **A single operational surface** for every Postgres in the production
  plane: one operator, one CRD, one runbook, one dashboard.
- **Declarative HA** (1 primary + 2 replicas with at least 1 synchronous)
  and automatic failover with no VIPs or external SPOFs.
- **Uniform backups and PITR** on top of Ceph RGW, reusing the S3 storage
  already provided by `infra/helm/infra/manifests/rook/`.
- **Bounded-context isolation** preserved: each service keeps its own
  `Cluster` and its own migrations under `services/<svc>/migrations/`.
- **Consistency with ADR-0007**: a single technology per stateful
  capability, reducing cognitive load and dependencies.
- **100% OSS** (Apache-2.0).

### Negative / trade-offs

- The data plane is coupled to Kubernetes and to the CNPG API.
- Baseline cost of 3 instances per service that requires strict HA;
  non-critical services may declare `instances: 1` with backups (no HA),
  but must justify it in their service-level ADR.
- Additional operational dependency on Ceph RGW for the backup/restore
  chain: an object-store outage degrades the WAL archive (not the write
  plane, thanks to local buffering).
- Learning curve for the `Cluster` CRD for teams that only know "plain"
  Postgres.

### Migration / cleanup

- The CNPG template under
  `infra/helm/infra/manifests/cnpg/templates/cluster.yaml`
  is the reference pattern. New service charts must reuse the same
  shape (`Cluster` CR + basic-auth `Secret`).
- There are no external Patroni instances or Postgres VIPs in
  `infra/helm/**` as of this ADR; there is nothing to retire.
- Any roadmap, prompt, or document mentioning "Patroni", "HAProxy for
  Postgres", or "externally managed Postgres" must link to this ADR
  and be rephrased in terms of CNPG.

## Conditions under which this decision would be reopened

This ADR must be reopened if **any** of the following conditions holds:

1. CNPG changes its licence, governance, or release cadence in a way
   that makes it incompatible with the project's OSS stance (Qdrant /
   OpenSearch precedent in ADR-0007).
2. A regulated deployment target mandates the use of a Postgres managed
   by a specific provider or a different certified operator.
3. A specific workload demonstrates, with reproducible benchmarks under
   `benchmarks/`, that CNPG cannot sustain the required RPO/RTO or
   throughput for a given bounded context.
4. We decide to consolidate Postgres into a shared multi-tenant cluster,
   which would break the "database-per-service" principle and would
   require reopening both this ADR and
   `docs/architecture/runtime-topology.md`.

## References

- `docs/architecture/runtime-topology.md` — "Postgres per service" model.
- `docs/architecture/adr/ADR-0007-search-engine-choice.md` — precedent
  for "a single stateful stack per capability" and OSS-licence filtering.
- `infra/helm/infra/manifests/cnpg/templates/cluster.yaml`
  — reference `Cluster` CR + basic-auth `Secret` pattern.
- `infra/helm/infra/manifests/rook/objectstore.yaml`, `infra/helm/infra/manifests/rook/bucket.yaml` —
  S3 provider for `s3://openfoundry-pg-backups/`.
- `services/*/migrations/` — per-service schemas (e.g.
  `services/cipher-service/migrations`; historical examples such as
  `services/data-asset-catalog-service/migrations` are pre-consolidation names,
  and current dataset/catalog ownership is in `dataset-versioning-service`).
- CloudNativePG: <https://cloudnative-pg.io/> (Apache-2.0).
- Barman / `barmanObjectStore`:
  <https://cloudnative-pg.io/documentation/current/backup_recovery/>.
