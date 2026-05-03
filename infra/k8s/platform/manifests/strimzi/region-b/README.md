# S7.3 — Kafka MirrorMaker 2 region A → region B

> Streaming-platform half of the cross-region DR plan
> ([migration-plan-cassandra-foundry-parity.md §12](../../../../../../docs/architecture/migration-plan-cassandra-foundry-parity.md)).
> Pairs with the Cassandra `dc-b1` work in S7.2 and the Postgres replica
> in S7.4 to give region B an up-to-date copy of every durable
> application stream.

## Files

| File | Purpose |
| ---- | ------- |
| [`kafka-cluster-region-b.yaml`](kafka-cluster-region-b.yaml) | Strimzi `KafkaNodePool` + `Kafka` for the region-B target cluster (`openfoundry-b`). |
| [`kafka-mirrormaker2.yaml`](kafka-mirrormaker2.yaml) | `KafkaMirrorMaker2` running in region A that replicates to `openfoundry-b` over an mTLS external listener. |

## Topology

```text
 region A (active)                       region B (warm DR)
+--------------------+                  +--------------------+
| Kafka openfoundry  |  ── MM2 (mTLS) ─▶| Kafka openfoundry-b|
| topics: cdc.*      |                  | topics: dc-a.cdc.* |
|         dataset.*  |                  |         dc-a.*     |
|         lineage.*  |                  |                    |
|         model.*    |                  |                    |
|         audit.*    |                  |                    |
+--------------------+                  +--------------------+
```

- **Source alias** `dc-a` → all replicated topics carry the `dc-a.`
  prefix on the target (S7.3.b). Names in region A stay clean.
- **MM2 deployment** is colocated with the source cluster (region A) so
  that an A-side outage takes MM2 down with it — there is nothing to
  replicate when A is gone, and consumers in B start from the last
  checkpoint translated by `MirrorCheckpointConnector`.

## What gets replicated

| Topic class | Pattern in A | Pattern in B | Replicated? |
| ----------- | ------------ | ------------ | ----------- |
| CDC | `cdc.<source>` | `dc-a.cdc.<source>` | yes |
| Dataset events | `dataset.changes` | `dc-a.dataset.changes` | yes |
| Lineage | `lineage.events` | `dc-a.lineage.events` | yes |
| Model inferences | `model.inferences` | `dc-a.model.inferences` | yes |
| Audit | `audit.events` | `dc-a.audit.events` | yes |
| MM2 internal | `mm2-*`, `*.checkpoints.internal`, `*.offsets.internal`, `heartbeats` | same | created/managed by MM2 |
| Anything matching `.*\.replica` | — | — | excluded (avoid recursive mirroring) |

Consumer-group offsets are translated every 30s by
`MirrorCheckpointConnector` into `dc-a.checkpoints.internal` on the
target. After a failover, applications in region B subscribe to
`dc-a.<topic>` and use the translated group offsets to resume without
re-processing.

## Secrets required

In region A namespace `kafka`:

- `mm2-source-tls` — KafkaUser TLS cert for MM2 against the local
  cluster `openfoundry`.
- `mm2-target-tls` — KafkaUser TLS cert issued by region B's CA, used
  by MM2 to authenticate to `kafka-b.openfoundry.example.com:9094`.
- `openfoundry-b-cluster-ca-cert` — public CA bundle of region B's
  cluster CA, used by MM2 to verify the broker certificate.

Provision via cert-manager + External Secrets; out of scope for this
manifest but the wiring is documented in
[`infra/runbooks/dr-failover.md`](../../../../../runbooks/dr-failover.md).

## SLOs

| Indicator | Target | Source |
| --------- | ------ | ------ |
| End-to-end replication lag (cdc.*) | p95 < 30s | `kafka_mm2_source_replication_latency_ms` |
| Heartbeat freshness | < 15s | `heartbeats` topic last record timestamp |
| Checkpoint freshness | < 60s | `kafka_mm2_checkpoint_checkpoint_latency_ms` |

The Prometheus rules are added in S7.5.b alongside the game-day
dashboard.

## Apply order

1. Apply `kafka-cluster-region-b.yaml` in the region-B context. Wait
   for the `Kafka openfoundry-b` resource to reach `Ready=True` and the
   external listener address to be assigned.
2. Wire DNS `kafka-b.openfoundry.example.com` to the LoadBalancer.
3. Provision the three Secrets above in region A.
4. Apply `kafka-mirrormaker2.yaml` in region A. Watch
   `kubectl -n kafka get kmm2 of-mm2-a-to-b -w` until `Ready=True`.
5. Smoke test: produce to `cdc.postgres` in A, consume from
   `dc-a.cdc.postgres` in B within ~30s.

## Failover semantics

This module only handles **data movement**. The actual cutover is
described in [`dr-failover.md`](../../../../../runbooks/dr-failover.md):
applications in region B resubscribe to `dc-a.*` topics, MM2 stops on
A's outage and consumer offsets are honoured from the translated
checkpoint stream. Failback (B → A) is the reverse and requires a
fresh MM2 instance running in region B; that path is exercised in the
S7.5.b game-day script.
