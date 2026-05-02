# Temporal cluster runbook (S2.1.g)

OpenFoundry runs **Apache Temporal** on top of the existing Cassandra
cluster (see [`infra/runbooks/cassandra.md`](cassandra.md)). The
deployment lives in [`infra/k8s/temporal/`](../k8s/temporal/) and is
managed by Flux. This runbook is the on-call entry point.

ADRs:
- [ADR-0020](../../docs/architecture/adr/ADR-0020-cassandra-as-operational-store.md) — Cassandra as operational store.
- [ADR-0021](../../docs/architecture/adr/ADR-0021-temporal-on-cassandra-go-workers.md) — Temporal + Go workers.
- [ADR-0012](../../docs/architecture/adr/ADR-0012-data-plane-slos.md) — data-plane SLOs.

## 1. Architecture (one paragraph)

Three replicas of each server tier (`frontend`, `history`, `matching`,
`worker`) run in the `temporal` namespace, anti-affinity-spread across
zones. Persistence is Cassandra (`temporal_persistence`,
`temporal_visibility`, NTS{dc1:3,dc2:3,dc3:3}, LOCAL_QUORUM). UI is
served by `temporalio/ui` behind oauth2-proxy + edge-gateway. Business
workers are **NOT** part of this deployment — they run in
`workers-go/*` (S2.2+).

## 2. Daily checks

```bash
# Server tiers all Ready.
kubectl -n temporal get pods -l app.kubernetes.io/instance=temporal -o wide

# Cluster reports namespaces and shard ownership.
kubectl -n temporal exec deploy/temporal-admintools -- \
  tctl --address temporal-frontend.temporal.svc.cluster.local:7233 \
  cluster health

# Schema versions match.
kubectl -n temporal exec deploy/temporal-admintools -- \
  tctl admin cluster describe
```

The Grafana dashboard `dp-temporal-overview` (UID once landed in the
dashboards repo) is the primary visual signal: shard distribution,
persistence latency, task queue lag.

## 3. Alerts → playbook

### `TemporalFrontendDown`

1. Check pod status and recent events:
   `kubectl -n temporal describe pod <pod>`.
2. Common causes: image pull failure, Cassandra TLS cert rotation,
   node drain. Cross-check `cert-manager` and the Cassandra
   `Issuer` for recent renewals.
3. If two of three frontends are down, page the on-call architect —
   the cluster is one frontend away from full read/write outage.

### `TemporalHistoryShardOwnershipFlapping`

1. Likely culprit is a flaky `history` pod or Cassandra `LOCAL_SERIAL`
   contention.
2. Inspect Cassandra read latency for `temporal_persistence` (SLI
   threshold in ADR-0012 §A.3).
3. Run a controlled rolling restart of `history`:
   `kubectl -n temporal rollout restart sts/temporal-history`.

### `TemporalCassandraPersistenceErrors`

1. Run `nodetool status` from any Cassandra pod; if any node is `DN`
   handle the Cassandra incident first.
2. If Cassandra is healthy, check `persistence_errors_with_type` per
   `operation` to identify the failing query class
   (RecordWorkflowExecutionStarted, ListWorkflowExecutions, etc).
3. The chart's schema upgrade Job can be re-run if a recent upgrade
   left the schema partially applied:
   `helm upgrade --reuse-values temporal temporal/temporal -n temporal`.

## 4. Common operations

### Bump the chart minor

The chart and the server image are tracked together. Renovate opens a
PR against `infra/k8s/temporal/values-prod.yaml` and
`helm-release.yaml`. After merge, Flux reconciles the upgrade.

### Rotate Cassandra credentials

The credentials live in
`secret/data/cassandra/temporal-user` (Vault) and are projected into
`temporal-cassandra-credentials` via external-secrets. Rotate by
updating Vault; the secret is reloaded on next pod restart. Frontends
are stateless so a rolling restart is safe.

### Drain a zone

`PodAntiAffinity` on zone means each tier survives losing one zone
(2 of 3 replicas remain). To drain:

```bash
for node in $(kubectl get nodes -l topology.kubernetes.io/zone=zone-a -o name); do
  kubectl drain "$node" --ignore-daemonsets --delete-emptydir-data
done
```

The 3-node Cassandra DC in the same zone (`dc1`) loses one node; the
cluster keeps `LOCAL_QUORUM` because it has 2 of 3 nodes per DC. The
SLOs of ADR-0012 §A.3 still apply.

## 5. Disaster recovery

* **Lost Cassandra zone**: Cassandra is RF=3 per DC. The Temporal
  schema is recreated by the `setup-schema` job after Cassandra
  recovers; workflow history rejoins automatically.
* **Lost Temporal namespace data**: irrecoverable from Temporal alone.
  Restore Cassandra from the most recent Medusa snapshot
  (`infra/runbooks/cassandra.md#restore-with-medusa`); the workflow
  state at that snapshot returns. Anything between the snapshot and
  the loss is gone.
* **Schema corrupted by a partial upgrade**: drop the keyspace
  (irreversible — confirm with the architecture group), re-run
  `cassandra-keyspaces-job.yaml`, then `helm upgrade` to re-run
  `setup-schema`.

## 6. Decommission

Per the README in `infra/k8s/temporal/`:

```bash
helm uninstall temporal -n temporal
# Optional: drop keyspaces (IRREVERSIBLE).
kubectl -n cassandra exec sts/of-cass-prod-dc1-rack1 -- cqlsh -e \
  "DROP KEYSPACE IF EXISTS temporal_persistence;
   DROP KEYSPACE IF EXISTS temporal_visibility;"
```

Take a Medusa snapshot first if there is any chance of needing the
workflow history back.
