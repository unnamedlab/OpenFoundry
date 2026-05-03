# Ceph RGW multisite — bootstrap & operations

> S7.1.a of the Cassandra/Foundry parity migration plan.
>
> Brings up an asynchronous master/secondary Ceph multisite topology
> spanning region A and region B so the Iceberg bucket namespace
> (`openfoundry-iceberg` and any sibling buckets in `rgw-data`) is
> replicated cross-region with native Ceph multisite (no S3 CRR rules
> required).

## Topology

```text
realm:     openfoundry
zonegroup: openfoundry-zg              (master in region A)
  ├── zone: openfoundry-zone-a         (PRIMARY, RW, region A)
  └── zone: openfoundry-zone-b         (SECONDARY, RO, region B)
```

Manifests:

* [`infra/k8s/platform/manifests/rook/multisite-region-a.yaml`](../k8s/platform/manifests/rook/multisite-region-a.yaml) — apply in region A.
* [`infra/k8s/platform/manifests/rook/multisite-region-b.yaml`](../k8s/platform/manifests/rook/multisite-region-b.yaml) — apply in region B **after** step 3 below.

Both reuse the existing `rgw-data` `CephObjectStore`
([objectstore.yaml](../k8s/platform/manifests/rook/objectstore.yaml)). The legacy
`openfoundry` store (datasets/models bucket) is **not** part of the
multisite — only `rgw-data` (Iceberg parquet/manifests) is replicated.

## Bootstrap order

### 1 · Region A — apply primary multisite

```sh
kubectl --context region-a -n rook-ceph apply -f infra/k8s/platform/manifests/rook/multisite-region-a.yaml
kubectl --context region-a -n rook-ceph rollout status deploy/rook-ceph-rgw-rgw-data
```

Wait for the toolbox pod to report a healthy period:

```sh
kubectl --context region-a -n rook-ceph exec deploy/rook-ceph-tools -- \
    radosgw-admin period get-current
```

### 2 · Region A — extract the pull token

The secondary zone authenticates against the master via a
per-zonegroup system user. Rook auto-creates it; harvest the
credentials with:

```sh
kubectl --context region-a -n rook-ceph exec deploy/rook-ceph-tools -- \
    radosgw-admin user info --uid=openfoundry-zg-system-user \
    -o jsonpath='{"access_key={.keys[0].access_key}\nsecret_key={.keys[0].secret_key}\n"}'
```

### 3 · Region B — install the pull token

```sh
kubectl --context region-b -n rook-ceph create secret generic rgw-multisite-pull-token \
    --from-literal=access-key="$ACCESS_KEY" \
    --from-literal=secret-key="$SECRET_KEY"
```

### 4 · Region B — apply secondary multisite

```sh
kubectl --context region-b -n rook-ceph apply -f infra/k8s/platform/manifests/rook/multisite-region-b.yaml
kubectl --context region-b -n rook-ceph rollout status deploy/rook-ceph-rgw-rgw-data
```

### 5 · Verify replication

In region B:

```sh
kubectl --context region-b -n rook-ceph exec deploy/rook-ceph-tools -- \
    radosgw-admin sync status
```

Expected:

* `metadata sync syncing` then `caught up with master` within ~30 s.
* `data sync source: <region-a-zone-id> syncing` then `caught up`
  within tens of seconds for empty buckets, longer once data flows.

## SLO

* RPO target ≤ 60 s for buckets in `rgw-data`. The smoke-test in
  S7.1.c (`infra/k8s/platform/manifests/lakekeeper/region-b/iceberg-replication-smoke.yaml`)
  asserts a write in region A is readable in region B within 60 s.
* RTO is irrelevant — region B is read-only by design; promotion
  requires the failover runbook (`dr-failover.md`, S7.5.a).

## Rollback / shrink

To remove region B without losing data:

1. Stop Lakekeeper region B (scale to 0).
2. Delete the secondary zone:
   `radosgw-admin zone delete --rgw-zone=openfoundry-zone-b --rgw-zonegroup=openfoundry-zg`.
3. Delete the secondary CephObjectStore.
4. `kubectl delete -f infra/k8s/platform/manifests/rook/multisite-region-b.yaml`.

The master zone in region A continues serving without interruption.
