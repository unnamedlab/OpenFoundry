# Vespa chart — sizing & operations

> Stream: S4 · Tarea S4.4
> Production target per [ADR-0028](../../../../../docs/architecture/adr/ADR-0028-search-backend-abstraction.md).
> Image: `vespaengine/vespa:8.450.40` (pinned in [`values.yaml`](values.yaml)).

## Production sizing (default `values-prod.yaml`)

| Tier | Replicas | CPU req / lim | Mem (heap) | Disk | StorageClass |
|------|----------|---------------|------------|------|--------------|
| **Configserver** | 3 (RAFT quorum, 1 per zone) | 0.5 / 1.0 | 1 Gi | 5 Gi | `ceph-rbd` |
| **Container** (stateless, query+feed) | 2 (HPA up to 6) | 1 / 4 | 2 Gi | — | — |
| **Content** (stateful, index storage) | 3 (1 per zone) | 2 / 8 | 2 Gi | 50 Gi | `ceph-rbd` |
| **redundancy** | 2 | — | — | — | — |
| **searchableCopies** | 1 | — | — | — | — |

* `redundancy=2`: every document is written to 2 content nodes.
  Survives 1 node loss with no read disruption.
* `searchableCopies=1`: only 1 copy is loaded into memory for query
  serving — the second is on-disk redundancy. Doubling this doubles
  RAM and halves restart time after a content-node loss.
* Topology spread: 1 pod per `topology.kubernetes.io/zone` per
  StatefulSet via `whenUnsatisfiable: DoNotSchedule`. PDB
  `minAvailable: configserver=2, content=2`.

## Capacity headroom assumptions

Sized for OpenFoundry's ontology cardinality target:

| Dimension | Target |
|-----------|--------|
| Indexed objects (single tenant) | 50 M |
| Indexed objects (cluster-wide) | 500 M |
| Avg doc size | 4 KiB JSON + 1.5 KiB inverted index entries |
| Disk per content node | 50 Gi (~30 % headroom over redundancy×avg) |
| Index memory per content node | 2 Gi heap + ~6 Gi off-heap (mmap) |
| Sustained index throughput | 5 000 docs/s/cluster |
| Sustained query throughput | 2 000 QPS/cluster |
| Lag SLO (consumer side) | P99 < 5 s — see [`ontology-indexer`](../../../../../services/ontology-indexer) |

## Scaling triggers

| Symptom | Action |
|---------|--------|
| Container CPU > 70 % for 10 min | bump container replicas (HPA already configured) |
| Content node disk > 70 % | add a content StatefulSet replica → Vespa rebalances automatically |
| Indexer lag P99 > 5 s and Kafka consumer lag is the bottleneck | bump container replicas (feed throughput) |
| Indexer lag P99 > 5 s and content-node CPU is the bottleneck | bump content node CPU `limits` |
| `vds.idealstate.merge_bucket.bucket_count > 0` for > 30 min | manual re-distribution; runbook below |

## Backup / DR

* **No backup of the index itself.** The index is a derived
  materialisation of Cassandra ontology storage; recovery means
  running the [reindex workflow](../../../../../workers-go/reindex)
  against the surviving region.
* Configserver state (deployed application package) is backed up
  via the standard Ceph RBD snapshot policy.

## Application package

The application package (`services.xml`, `hosts.xml.tmpl`,
`schemas/*.sd`) is bundled into a `ConfigMap` by
[`templates/configmap-app.yaml`](templates/configmap-app.yaml) from
the source tree at `infra/k8s/platform/packages/vespa-app/`. Updates are deployed by
the `Job` in [`templates/job-deploy.yaml`](templates/job-deploy.yaml)
which runs `vespa-deploy prepare && vespa-deploy activate`.

## Runbooks

* **Add a content node**: bump `content.replicas` in
  `infra/k8s/platform/values/vespa-prod.yaml`, then run
  `helmfile -e prod apply` from `infra/k8s/platform`. Vespa rebalances buckets automatically. Watch
  `vds.idealstate.merge_bucket.pending` until 0.
* **Lose a content node**: PDB blocks node-drain past `minAvailable=2`.
  K8s reschedules the StatefulSet pod, Vespa re-replicates from the
  surviving copy. ETA: ~15 min for a full 50 Gi pod.
* **Full cluster recovery**: redeploy the `vespa` platform release, then run
  the OntologyReindex workflow once per tenant.

## Dev / CI

`vespa.enabled=false` in [`../../values/vespa-dev.yaml`](../../values/vespa-dev.yaml).
The dev stack uses OpenSearch via
[`libs/search-abstraction`](../../../../../libs/search-abstraction);
both backends share the same trait so application code does not see
the difference.
