# Runbook — Cross-region DR failover (region A → region B)

> S7.5.a of the Cassandra/Foundry parity migration plan.
>
> Master sequencing document for a full region-A outage. Composes the
> per-component runbooks
> ([cassandra-app-failover.md](cassandra-app-failover.md),
> [postgres-promotion.md](postgres-promotion.md),
> [ceph-multisite-bootstrap.md](ceph-multisite-bootstrap.md))
> into an end-to-end procedure. Pair with the game-day script
> [dr-game-day.md](dr-game-day.md) for the rehearsed exercise.

## Targets

| Indicator | Target | Notes |
| --------- | ------ | ----- |
| **RTO** (recovery time objective) | ≤ 30 min | From declared incident to first user-served request in B. |
| **RPO** for Cassandra app data | 0 | Multi-master, sync writes. |
| **RPO** for Postgres | ≤ 60 s | Async streaming, measured by replay lag at promotion. |
| **RPO** for Kafka topics | ≤ 30 s | MM2 source connector lag. |
| **RPO** for Iceberg / Lakekeeper | ≤ 60 s | Ceph multisite RGW sync. |

## Activation

Declare a DR event when **any** of the following holds for > 5 min and
the on-call IC has confirmed it cannot be resolved by routine
recovery:

* Region-A Kubernetes API server unreachable from the global edge.
* Region-A Postgres primaries unreachable from region B (lag ∞).
* Majority of region-A Cassandra DCs (`dc1/dc2/dc3`) `Stopped` /
  `NotReady`.
* Region-A Ceph cluster reports `HEALTH_ERR` with mons down.

Do **not** activate this runbook for partial degradation; the warm
standbys in B are read-only by design and a needless cutover forces a
mandatory failback bootstrap.

## Pre-flight checklist (5 min)

- [ ] DR coordinator on the bridge.
- [ ] Postmortem doc created, timestamp recorded.
- [ ] `kubectl --context region-b` works.
- [ ] Region B health snapshot:
  ```sh
  kubectl --context region-b get cluster -A
  kubectl --context region-b -n cassandra get cassandradatacenter dc-b1
  kubectl --context region-b -n kafka get kafka openfoundry-b
  kubectl --context region-b -n rook-ceph get cephcluster rook-ceph
  ```
- [ ] All four CNPG replicas show `pg_is_in_recovery=t` with lag < 60s.
- [ ] MM2 metrics: `kafka_mm2_source_replication_latency_ms` p95 < 60s.
- [ ] Multisite RGW: `radosgw-admin sync status` reports `caught up`.

## Step 0 — Stop accepting writes in A (if reachable)

If region A's API still answers but is degrading, scale write-path
services to zero to give the standbys a clean WAL boundary:

```sh
kubectl --context region-a -n openfoundry scale deploy --all --replicas=0
```

If region A is fully unreachable, skip to Step 1.

## Step 1 — Promote Postgres standbys

Follow [postgres-promotion.md](postgres-promotion.md). One command per
cluster:

```sh
for c in pg-schemas-replica pg-policy-replica \
         pg-runtime-config-replica pg-lakekeeper-replica; do
    kubectl --context region-b -n openfoundry patch cluster "$c" \
        --type merge -p '{"spec":{"replica":{"enabled":false}}}'
done
```

**Wait** for `pg_is_in_recovery=f` on all four before proceeding to
Step 2. Postgres write availability is a hard dependency for every
service in the stack.

## Step 2 — Switch Cassandra LOCAL_QUORUM to `dc-b1`

Follow [cassandra-app-failover.md](cassandra-app-failover.md):

```sh
cd infra/k8s/helm && \
    HELM_KUBECONTEXT=region-b \
    helmfile -e prod apply \
    --state-values-set global.cassandra.localDc=dc-b1
```

This rolling-restarts every Deployment with `CASSANDRA_LOCAL_DC=dc-b1`.

## Step 3 — Repoint Kafka consumers to `dc-a.*` topics

Region-B services discover their topic prefix from the
`KAFKA_TOPIC_PREFIX` env var (defaults to `""`). On failover, set it
to `dc-a.`:

```sh
cd infra/k8s/helm && \
    HELM_KUBECONTEXT=region-b \
    helmfile -e prod apply \
    --state-values-set global.kafka.topicPrefix=dc-a. \
    --state-values-set global.kafka.bootstrap=openfoundry-b-kafka-bootstrap.kafka.svc:9093
```

`MirrorCheckpointConnector` has been writing translated consumer
group offsets every 30s, so consumers resume from the right point
without re-processing.

## Step 4 — Promote Lakekeeper region B to RW (lakehouse only)

Lakekeeper region-B is permanently RO during steady state (S7.1.b).
Promotion to RW is documented in
[`../k8s/platform/manifests/lakekeeper/region-b/README.md`](../k8s/platform/manifests/lakekeeper/region-b/README.md):

```sh
helm --kube-context region-b upgrade lakekeeper \
    quay.io/lakekeeper/charts/lakekeeper \
    -f infra/k8s/platform/manifests/lakekeeper/values.yaml \
    -f infra/k8s/platform/manifests/lakekeeper/region-b/values-region-b.yaml \
    --set authz.backend=allow_all \
    --set externalDatabase.host_write=pg-lakekeeper-replica-rw \
    --reuse-values
```

Also flip the Ceph zone to master via radosgw:

```sh
kubectl --context region-b -n rook-ceph exec deploy/rook-ceph-tools -- \
    radosgw-admin zone modify --rgw-zone=openfoundry-zone-b --master --default && \
    radosgw-admin period update --commit
```

## Step 5 — Promote Temporal frontend to region B

Temporal's persistence sits on `pg-policy-replica` and Cassandra
`dc-b1`. Both are now writeable. Scale the Temporal frontend in B:

```sh
kubectl --context region-b -n temporal scale deploy temporal-frontend --replicas=3
kubectl --context region-b -n temporal scale deploy temporal-history --replicas=3
kubectl --context region-b -n temporal scale deploy temporal-matching --replicas=3
kubectl --context region-b -n temporal scale deploy temporal-worker --replicas=3
```

Verify:

```sh
kubectl --context region-b -n temporal exec deploy/temporal-admintools -- \
    tctl --address temporal-frontend.temporal.svc:7233 cluster health
```

## Step 6 — Switch the global edge

Two options depending on the DNS layer in use:

* **Route53 / CloudDNS health-check failover record set**: flip
  weight from A to B (TTL 30s).
* **Anycast / global LB control plane**: drain the A backend pool and
  promote the B pool to active.

Then verify:

```sh
curl -fsS https://api.openfoundry.example.com/healthz
# Expected: 200 OK with X-Region: b header
```

## Step 7 — Verify the user-facing surface

```sh
# Auth (identity-federation-service)
curl -fsS https://api.openfoundry.example.com/api/v1/auth/health

# Ontology read path
curl -fsS https://api.openfoundry.example.com/api/v1/ontology/objects?limit=1

# Workflow (Temporal) write path
curl -fsS -X POST https://api.openfoundry.example.com/api/v1/workflows/health-probe
```

Watch the Grafana dashboard `dr-overview` (region B):

* P95 read latency stable inside SLO within 5 min.
* 5xx error rate < 1% within 5 min.
* Temporal workflow start rate non-zero.

## Step 8 — Pin the failover state in Git

Open a single PR `dr/failover-<UTC-timestamp>` that:

1. Sets `replica.enabled: false` for the promoted CNPG clusters in
   [`cnpg-replicas-region-b.yaml`](../k8s/platform/manifests/cnpg/region-b/cnpg-replicas-region-b.yaml).
2. Updates [`values-prod.yaml`](../k8s/helm/profiles/values-prod.yaml):
   `global.cassandra.localDc=dc-b1`,
   `global.kafka.topicPrefix=dc-a.`,
   `global.kafka.bootstrap=openfoundry-b-kafka-bootstrap...`.
3. Rotates the affected `<bc>-db-dsn` Secrets / External Secrets inputs
   so `DATABASE_URL` and `DATABASE_READ_URL` resolve to
   `*-replica-rw/-ro` per [`DATABASE_URL.md`](../k8s/helm/DATABASE_URL.md).
4. Updates the active region label in the DR dashboard under
   `infra/k8s/platform/observability/grafana-dashboards/`.
5. Embeds the incident timestamp and post-mortem link in the PR
   description.

Merge with the on-call IC's approval; do not rely on standard CODEOWNERS
review during an incident.

## Step 9 — Failback (after region A recovery)

Failback is the reverse of Steps 1–8 with each component's per-runbook
failback procedure:

1. Bootstrap region A as a fresh CNPG replica of region B's promoted
   primaries ([postgres-promotion.md](postgres-promotion.md) Step 7).
2. Wait for sync and full repair across Cassandra DCs
   ([cassandra-app-failover.md](cassandra-app-failover.md) Step 6).
3. Spin up a new MM2 replicating B → A with source alias `dc-b`.
4. Wait for Ceph multisite to re-sync (`radosgw-admin sync status` =
   `caught up`).
5. Reverse Steps 6 → 1 (DNS first to drain B, then component
   demotions).
6. Revert the Step 8 PR.

Failback is **always** a planned, scheduled maintenance window. Never
fail back during an incident.

## Non-goals

* This runbook does not cover **regional DR for the Ceph cluster
  itself** (the underlying block storage). RBD CRR for stateful
  workloads is tracked separately in S8.
* This runbook does not cover **secret rotation across regions**;
  that lives in the cert-manager / External Secrets pipeline and
  follows its own cadence.
* This runbook does not cover **reconciling user-visible IDs** that
  rely on region-local sequences. ULIDs and snowflake-style IDs are
  region-prefixed and safe; if any service still uses bigserial, fix
  it in S8 before relying on this runbook in production.
