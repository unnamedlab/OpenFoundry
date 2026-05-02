# Runbook — Cassandra application failover to `dc-b1` (region B)

> S7.2.d of the Cassandra/Foundry parity migration plan.
>
> Promotes the region-B datacenter (`dc-b1`) to the active serving DC
> after a region-A outage, by switching every OpenFoundry service from
> `LOCAL_QUORUM @ dc1` to `LOCAL_QUORUM @ dc-b1`. Cassandra itself is
> always multi-master across DCs — this runbook only changes which DC
> the **application** routes to.

## Topology recap

```text
of-cass-prod (single logical cluster, NetworkTopologyStrategy)
  region A: dc1 (3 nodes), dc2 (3), dc3 (3)
  region B: dc-b1 (3 nodes)         ← promotion target
```

* Replication factor: `dc1:3, dc2:3, dc3:3, dc-b1:3` on every
  application keyspace ([keyspaces-job.yaml](../k8s/cassandra/keyspaces-job.yaml)).
* Application default consistency: `LOCAL_QUORUM` (per ADR-0020 / 0021).
* Driver: [`scylla`](https://docs.rs/scylla) Rust crate; `local_dc` is
  configured via `CASSANDRA_LOCAL_DC` environment variable wired into
  every service's `cassandra-kernel` initialisation.

## When to fail over

Trigger this runbook when **any** of the following holds:

* Region A control plane is unreachable from the global edge for > 5 min.
* All three region-A DCs (`dc1/dc2/dc3`) are simultaneously down at
  Cassandra level (operator status `Stopped` or majority of pods
  `NotReady`).
* The DR drill schedule explicitly asks for a planned promotion
  ([game-day playbook](dr-failover.md), S7.5.b).

Do **not** fail over for partial region-A degradation — `LOCAL_QUORUM`
in region A continues to serve as long as at least one DC has 2/3 nodes
healthy, and a needless cross-region promotion adds latency without
upside.

## Step 0 — Pre-flight (30 s)

```sh
# Region B Cassandra healthy?
kubectl --context region-b -n cassandra get cassandradatacenter dc-b1 \
    -o jsonpath='{.status.cassandraOperatorProgress}{"\n"}'
# Expected: Ready

# No active repair on dc-b1 (otherwise QUORUM may stall on streaming).
kubectl --context region-b -n cassandra exec sts/of-cass-prod-dc-b1-default-sts-0 -c cassandra -- \
    nodetool tpstats | grep -E 'AntiEntropyStage|RepairTask'
```

## Step 1 — Cut application traffic to region B (2 min)

The `CASSANDRA_LOCAL_DC` env var is set per-service in the Helm
umbrella. Override it cluster-wide via the platform values overlay:

```sh
helm --kube-context region-b upgrade open-foundry \
    infra/k8s/helm/open-foundry \
    -f infra/k8s/helm/open-foundry/values.yaml \
    -f infra/k8s/helm/open-foundry/values-prod.yaml \
    --set globals.cassandra.localDc=dc-b1 \
    --reuse-values
```

This re-renders every service's Deployment with
`CASSANDRA_LOCAL_DC=dc-b1`. The driver picks up the change on pod
restart (rolling — RBAC + PodDisruptionBudgets keep at least 50% of
each Deployment up).

## Step 2 — Switch the global edge / DNS (1 min)

Point the public hostname (`api.openfoundry.example.com`) at the
region-B ingress. Use whichever mechanism is in place:

* **Route53 / CloudDNS health check failover**: flip the active record
  set to region B (TTL 30 s; expect ≤ 60 s propagation).
* **Anycast / global LB** (preferred at scale): drain region A in the
  LB control plane.

## Step 3 — Verify quorum is healthy on `dc-b1` (1 min)

```sh
kubectl --context region-b -n cassandra exec sts/of-cass-prod-dc-b1-default-sts-0 -c cassandra -- \
    nodetool status | awk '/^Datacenter:/{dc=$2} /^UN/{print dc, $2, $7}'
# Expected: 3 lines for dc-b1, status UN, load balanced.

# Application read smoke test from any service in region B.
kubectl --context region-b -n openfoundry exec deploy/identity-federation-service -- \
    cqlsh of-cass-prod-dc-b1-service.cassandra.svc -u "$U" -p "$P" \
    -e "CONSISTENCY LOCAL_QUORUM; SELECT count(*) FROM auth_runtime.sessions LIMIT 1;"
```

## Step 4 — Confirm error budget (5 min observation)

Watch the Grafana dashboard `cassandra-overview` (region B):

* P95 read latency on `LOCAL_QUORUM` < 20 ms (warm cache may take 1–2
  min after pod restarts).
* Read errors / sec ≈ 0 (transient `RequestHandlerException` during
  driver reconnect is expected, < 5 / min).

## Step 5 — Pin the promotion in the platform state

Commit the values-prod overlay change so the next `helm upgrade` does
not silently revert:

```sh
git checkout -b dr/failover-to-dc-b1
sed -i.bak 's/localDc: dc1/localDc: dc-b1/' infra/k8s/helm/open-foundry/values-prod.yaml
git add -A && git commit -m "dr: pin Cassandra LOCAL_QUORUM to dc-b1 (region B)"
```

Open the PR with the incident timestamp in the description so the
post-mortem and the rollback PR (Step 6) are easy to correlate.

## Step 6 — Rollback to region A (after region-A recovery)

After region A is fully restored:

1. Run anti-entropy repair on region A keyspaces:
   ```sh
   kubectl --context region-a -n cassandra exec deploy/of-cass-prod-reaper -- \
       curl -X POST "http://localhost:8080/repair_run?clusterName=of-cass-prod&keyspace=ALL&datacenters=dc1,dc2,dc3&owner=dr-rollback&parallelism=PARALLEL&intensity=0.6&repairType=FULL"
   ```
2. Wait for repair `DONE` per keyspace (Reaper UI or
   [`cross-dc-repair-job.yaml`](../k8s/cassandra/cross-dc-repair-job.yaml)
   adapted to target `dc1,dc2,dc3`).
3. Reverse Step 2 (DNS) and Step 1 (Helm `localDc=dc1`).
4. Revert the PR from Step 5.

## Non-goals

* This runbook does **not** promote any other component. Postgres
  promotion lives in [`dr-failover.md`](dr-failover.md) (S7.5.a),
  Lakekeeper region-B is permanently read-only ([region-B README](../k8s/lakekeeper/region-b/README.md)),
  Kafka MM2 sequencing is owned by S7.3.

* This runbook does **not** modify `system_auth` replication. The
  superuser keyspace is replicated identically across all 4 DCs by the
  operator; any auth changes propagate via gossip (subject to repair
  cadence — see ADR-0021).
