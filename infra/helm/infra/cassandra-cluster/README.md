# Cassandra operational store (k8ssandra-operator)

OpenFoundry's hot operational store is Apache Cassandra 5, deployed and
operated through the **k8ssandra-operator** umbrella. This README mirrors
the convention used by [`infra/k8s/strimzi`](../strimzi/README.md) and
[`infra/k8s/rook`](../rook/README.md): the upstream operator is installed
from its public Helm chart, and only the *desired CRs* are checked into
the repository.

For the rationale (why Cassandra at all, why k8ssandra-operator over
direct cass-operator, why these particular consistency / compaction /
modelling rules), see:

- [ADR-0020](../../../docs/architecture/adr/ADR-0020-cassandra-as-operational-store.md)
- [ADR-0021](../../../docs/architecture/adr/ADR-0021-temporal-on-cassandra-go-workers.md)
  — historical only; superseded by [ADR-0037](../../../docs/architecture/adr/ADR-0037-foundry-pattern-orchestration.md).
  The Temporal-on-Cassandra direction was retired in FASE 9 of the
  Foundry-pattern migration; this cluster no longer hosts the
  `temporal_persistence` / `temporal_visibility` keyspaces.
- [docs/architecture/data-model-cassandra.md](../../../docs/architecture/data-model-cassandra.md)

| Component                | License    | Role                                             |
|--------------------------|------------|--------------------------------------------------|
| k8ssandra-operator       | Apache-2.0 | Umbrella operator (clusters, repair, backup)     |
| cass-operator            | Apache-2.0 | Manages the Cassandra StatefulSet (sub-operator) |
| Apache Cassandra 5.0 LTS | Apache-2.0 | Operational store                                |
| Cassandra Reaper         | Apache-2.0 | Scheduled / triggered repairs (sub-operator)     |
| Medusa                   | Apache-2.0 | Backups to object storage (sub-operator)         |

**Why k8ssandra-operator over plain cass-operator.** k8ssandra-operator
is the official DataStax umbrella that wraps cass-operator and adds
first-class CRs for Reaper (anti-entropy repair) and Medusa (backup to
S3-compatible storage), both of which we need from day one. Stargate is
**not** enabled — services talk Cassandra-native via the `scylla` Rust
crate ([ADR-0020](../../../docs/architecture/adr/ADR-0020-cassandra-as-operational-store.md)),
and Stargate's REST/GraphQL surface would be a parallel access path we
do not want.

> **Note on the Rust driver.** The `scylla` crate is the chosen driver
> for both ScyllaDB and Apache Cassandra; it speaks CQL natively and
> outperforms the legacy `cassandra-cpp` bindings without dragging in
> a C++ build dependency. Naming notwithstanding, the runtime backend
> is Apache Cassandra per
> [ADR-0020](../../../docs/architecture/adr/ADR-0020-cassandra-as-operational-store.md).

## Files

| File                                  | Purpose                                                                       |
|---------------------------------------|-------------------------------------------------------------------------------|
| `values-k8ssandra-operator.yaml`      | Helm values for the upstream `k8ssandra/k8ssandra-operator` chart             |
| `cluster-dev.yaml`                    | Single-DC `K8ssandraCluster` (3 nodes, RF=3) for `dev` / `ci`                  |
| `cluster-prod.yaml`                   | Multi-DC production cluster (3 DCs × 3 nodes, RF=3 per DC) — added in S0.2.c  |

The k8ssandra-operator **chart itself** is not vendored here. Only the
`K8ssandraCluster` CRs and the chart values file are version-controlled,
matching the convention used for Strimzi.

## Storage

Cassandra data volumes are placed on the **`ceph-rbd-fast`** StorageClass,
backed by the `rbd-fast` CephBlockPool defined in
[`infra/k8s/rook/cluster.yaml`](../rook/cluster.yaml) (NVMe-only OSDs,
3-way replication, zone failure domain). The StorageClass is provisioned
out-of-band per the same convention documented in the Rook README. JBOD
is **not** used at the Ceph layer (Cassandra sees a single `data`
volume per pod whose backing device is already triple-replicated and
striped by Ceph); the term "JBOD" in the migration plan refers to the
on-host storage shape Cassandra exposes to itself, where each Cassandra
node has one or more data directories with no underlying RAID.

## Apply order

```bash
# 0. Prereqs: cert-manager (for the operator's webhook certs) and Rook Ceph
#    must already be installed and the `ceph-rbd-fast` StorageClass
#    must be provisioned.

# 1. k8ssandra-operator (cluster-scoped CRDs, namespace-scoped controller).
helm repo add k8ssandra https://helm.k8ssandra.io/stable
helm repo update
helm install k8ssandra-operator \
  k8ssandra/k8ssandra-operator \
  --namespace k8ssandra-operator \
  --create-namespace \
  -f infra/k8s/cassandra/values-k8ssandra-operator.yaml

kubectl -n k8ssandra-operator wait --for=condition=Available \
  deployment/k8ssandra-operator --timeout=5m

# 2. Dev / CI cluster (single-DC, 3 nodes, RF=3).
kubectl create namespace cassandra --dry-run=client -o yaml | kubectl apply -f -
kubectl apply -f infra/k8s/cassandra/cluster-dev.yaml
kubectl -n cassandra wait --for=condition=CassandraInitialized \
  k8ssandracluster/of-cass-dev --timeout=20m

# 3. (Production) Multi-DC cluster — added in S0.2.c.
# kubectl apply -f infra/k8s/cassandra/cluster-prod.yaml
```

## Smoke test

```bash
# Open a cqlsh shell against the dev cluster.
kubectl -n cassandra exec -it of-cass-dev-dc1-default-sts-0 -c cassandra -- \
  cqlsh -u cassandra-superuser \
        -p "$(kubectl -n cassandra get secret of-cass-dev-superuser \
              -o jsonpath='{.data.password}' | base64 -d)"

# Inside cqlsh:
DESCRIBE KEYSPACES;
SELECT cluster_name, listen_address, release_version FROM system.local;
```

## Operational notes

- **Repairs** are scheduled by Reaper (one full repair per keyspace per
  week, sub-range parallelism). The Reaper schedule is defined in the
  `K8ssandraCluster` CR.
- **Backups** are taken by Medusa to a Ceph S3 bucket (`cassandra-backups`,
  see [`infra/k8s/rook/bucket.yaml`](../rook/bucket.yaml) for the
  bucket-claim pattern). Schedule and retention live in the
  `K8ssandraCluster` CR.
- **Schema migrations** are owned by the application crates that own
  each keyspace; the operator does **not** create application keyspaces
  itself. `cqlsh` migration runners are invoked from each owning service
  at startup, gated by an init container.

## Related

- [ADR-0020](../../../docs/architecture/adr/ADR-0020-cassandra-as-operational-store.md)
  — adoption decision, hard modelling rules.
- [ADR-0021](../../../docs/architecture/adr/ADR-0021-temporal-on-cassandra-go-workers.md)
  — Superseded by [ADR-0037](../../../docs/architecture/adr/ADR-0037-foundry-pattern-orchestration.md).
  Temporal used to share this cluster for `temporal_persistence`
  and `temporal_visibility`; both keyspaces were dropped during the
  FASE 9 cutover ([runbook](../../../infra/runbooks/temporal.md)).
- [ADR-0037](../../../docs/architecture/adr/ADR-0037-foundry-pattern-orchestration.md)
  — Foundry-pattern orchestration; the migration that retired
  the Temporal direction recorded in ADR-0021.
- [docs/architecture/data-model-cassandra.md](../../../docs/architecture/data-model-cassandra.md)
  — keyspace and table design.
