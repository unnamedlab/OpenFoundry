# ADR-0012 And Closure Runbooks Evidence - 2026-05-03

Status: BLOCKED
Outcome: NOT APPROVED FOR FINAL CLOSURE

This pack records the final evidence collection attempt for ADR-0012 and
the closure runbooks that gate S1, S3 and S5. It is intentionally not a
PASS pack: the connected Kubernetes context does not contain the runtime
systems required to collect production-grade measurements.

## Run Metadata

| Field | Value |
|---|---|
| Owner | OpenFoundry platform architecture group |
| Operator | Codex local automation, on behalf of the repo owner |
| Local timestamp | 2026-05-03T22:04:08+0200 |
| Kubernetes context | `default` |
| Git SHA | `9d00721` |
| Workspace state | Dirty; pre-existing and current documentation/code changes present |
| Approval status | No exception is approved in this pack |

## Environment Discovery

The current context is a lightweight local cluster. It is not the S1/S3/S5
target environment.

```bash
kubectl config current-context
```

Result:

```text
default
```

```bash
kubectl get ns
```

Result:

```text
NAME                 STATUS   AGE
cert-manager         Active   13h
cnpg-system          Active   13h
default              Active   2d22h
k8ssandra-operator   Active   13h
kube-node-lease      Active   2d22h
kube-public          Active   2d22h
kube-system          Active   2d22h
openfoundry          Active   12h
registry             Active   5h11m
```

```bash
kubectl get deploy -A
```

Result summary:

```text
openfoundry/authorization-policy-service   0/1
openfoundry/edge-gateway-service           0/1
openfoundry/identity-federation-service    0/1
openfoundry/tenancy-organizations-service  0/1
openfoundry/web                            1/1
```

```bash
kubectl get cassandradatacenters -A
kubectl get clusters.postgresql.cnpg.io -A
kubectl get statefulset -A
kubectl get cronjobs -A
```

Result:

```text
No CassandraDatacenter resources found.
No CNPG Cluster resources found.
No StatefulSet resources found.
No CronJob resources found.
```

`kubectl api-resources` also shows no Strimzi Kafka resources, no
Prometheus Operator resources and no SparkApplication resources.

## Metric Closure Matrix

| Required measurement | Required evidence | Current result | Closure status |
|---|---|---|---|
| Latency p50/p95/p99 for ontology S1 | `benchmarks/ontology/scripts/run-s1-baseline.sh` artifacts from a 3-node Cassandra cluster plus `nodetool tablestats` snapshots | Blocked: no CassandraDatacenter, no ontology services ready, `k6` not installed locally | OPEN |
| Throughput for ontology S1 | k6 5,000 RPS run with dropped iterations and error rate | Blocked by same S1 environment gap | OPEN |
| Error rate for ontology S1 | k6 `http_req_failed` plus service error counters | Blocked by same S1 environment gap | OPEN |
| Kafka lag for S5 sinks | Kafka consumer-group offsets and Prometheus lag panels | Blocked: no Kafka namespace/CRDs/runtime | OPEN |
| Temporal latency | Temporal frontend/workers deployed with SDK metrics or Temporal visibility timings | Blocked in this context: no Temporal namespace/runtime | OPEN |
| Iceberg write latency | Sink append latency histograms and Iceberg row-count deltas | Blocked: no Lakekeeper/Iceberg/S5 sink runtime | OPEN |
| Trino query latency | `sql-bi-gateway-service` routing to Trino plus Trino query output | Blocked: no Trino/Lakekeeper/sql-bi-gateway runtime | OPEN |
| S3 identity failover/DR | Signed `identity-failover-drill` run with k6/Locust, Grafana and Cassandra/Vault/Redis/Kafka evidence | Blocked: identity is 0/1 and required dependencies are absent | OPEN |
| S5 restart recovery | `lakehouse-evidence/<date>/restart-drill.txt` with sink restarts and catch-up | Existing 2026-05-03 pack is BLOCKED, not PASS | OPEN |
| CNPG decommission closure | Signed `cnpg-decommission` run proving exactly four consolidated clusters and no legacy CRs/PVCs | Blocked: no CNPG clusters exist in this context, so the target decommission cannot be verified | OPEN |

## Exception Register

No exception is approved in this pack.

| Exception id | Scope | Requested exception | Approval status | Required approvers |
|---|---|---|---|---|
| `EXC-S1-ADR0012-2026-05-03` | ADR-0012 S1 latency/throughput/error measurements | Allow ADR-0012 to remain without numeric S1 baseline until a real Cassandra benchmark environment exists | NOT APPROVED | Platform architecture group + SRE on-call |
| `EXC-S3-DR-2026-05-03` | Identity pen-test and failover drill | Allow S3 closure without executed security/failover evidence | NOT APPROVED | Security architect + identity maintainer + SRE on-call |
| `EXC-S5-OPS-2026-05-03` | Lakehouse operational evidence | Allow S5 closure without Kafka/Iceberg/Trino/Spark runtime evidence | NOT APPROVED | Data platform maintainer + SRE on-call |
| `EXC-S6-CNPG-2026-05-03` | CNPG decommission sign-off | Allow final Postgres residual closure without a decommission run in the target environment | NOT APPROVED | Database owner + SRE on-call |

## Required Re-run Commands

S1:

```bash
kubectl apply -f infra/k8s/bench/ontology-bench-namespace.yaml
kubectl apply -f infra/k8s/bench/ontology-bench-credentials.yaml
kubectl apply -f infra/k8s/bench/ontology-bench-seed-job.yaml
kubectl -n openfoundry-bench create job --from=cronjob/ontology-bench-k6 ontology-bench-k6-$(date +%Y%m%d-%H%M)
benchmarks/ontology/scripts/run-s1-baseline.sh
```

S3:

```bash
docs/architecture/runbooks/identity-pen-test-runbook.md
docs/architecture/runbooks/identity-failover-drill.md
```

S5:

```bash
docs/architecture/runbooks/lakehouse-s5-operational-evidence.md
```

S6 CNPG:

```bash
infra/runbooks/cnpg-decommission.md
```

## Final Result

This pack is valid as evidence that final closure was attempted and
blocked by environment availability. It is not valid evidence that the
SLOs or runbooks passed.
