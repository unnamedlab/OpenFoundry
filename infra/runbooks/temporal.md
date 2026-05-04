# Temporal — retired (FASE 9 / Tarea 9.2 cutover runbook)

> **Status: retired.** OpenFoundry no longer runs Apache Temporal.
> ADR-0027 (Foundry-pattern orchestration) replaced the centralised
> Temporal cluster with per-domain Postgres state machines, the
> transactional outbox + Debezium, Kafka topics, and SparkApplication
> CRs for batch. This runbook is now the **decommission and Cassandra
> keyspace cleanup** guide; it is what an operator follows once the
> workloads have been cut over and the cluster is safe to remove.

ADRs:

- [ADR-0027 — Foundry-pattern orchestration](../../docs/architecture/adr/ADR-0027-foundry-pattern-orchestration.md) — the retirement decision.
- [ADR-0021 — Temporal + Go workers](../../docs/architecture/adr/ADR-0021-temporal-on-cassandra-go-workers.md) — the original ADR (kept as audit trail).
- [ADR-0020 — Cassandra as operational store](../../docs/architecture/adr/ADR-0020-cassandra-as-operational-store.md) — Cassandra remains the substrate for the ontology; only the Temporal-specific keyspaces leave.

Per-task replacements (FASE 3 – 7 of the migration plan):

| Workload | Replacement | Migration task |
|---|---|---|
| `pipeline-worker` | `services/pipeline-build-service` (SparkApplication CR submitter) + `schedules-tick` CronJob | Tarea 3.6 |
| `reindex/` | `services/reindex-coordinator-service` (Kafka-driven, Postgres-resumable) | Tarea 4.3 |
| `workflow-automation-worker` | `services/workflow-automation-service` (condition consumer + outbox) | Tarea 5.4 |
| `automation-ops-worker` | `services/automation-operations-service` (saga consumer + `libs/saga::SagaRunner`) | Tarea 6.5 |
| `approvals-worker` | `services/approvals-service` (`audit_compliance.approval_requests` state machine) + `approvals-timeout-sweep` CronJob | Tarea 7.5 |

The Cassandra `temporal_persistence` and `temporal_visibility`
keyspaces remain after the retirement until an operator runs the
DROP step below.

---

## 1. Pre-flight

> **Run these checks BEFORE the destructive DROP. Stop here if any
> of them fail.**

### 1.1 Confirm no Temporal workloads remain

```bash
# No `temporal` namespace, no Temporal pods anywhere in the cluster.
kubectl get ns temporal 2>/dev/null && echo "WARN: temporal namespace still exists"

# No Go Temporal workers in any namespace.
kubectl get deploy --all-namespaces \
  -l 'app.kubernetes.io/name in (pipeline-worker,workflow-automation-worker,approvals-worker,automation-ops-worker)' \
  -o name
```

Both queries must return no rows.

### 1.2 Confirm the helm chart wrapper is gone

```bash
# Tarea 9.1 deletes the wrapper chart; this should produce nothing.
ls infra/helm/infra/temporal 2>/dev/null
```

### 1.3 Confirm Rust services no longer carry the dep

```bash
# Should produce nothing — the workspace dep was retired by FASE 8.
grep -l 'temporal-client' services/*/Cargo.toml libs/*/Cargo.toml 2>/dev/null
```

### 1.4 Take a Medusa snapshot (only if you might want history back)

```bash
# Only if there is ANY chance of legal / compliance recovery —
# once the keyspaces are dropped the workflow history is gone.
kubectl -n cassandra exec sts/of-cass-prod-dc1-rack1 -- \
  medusa backup --backup-name pre-temporal-decommission-$(date +%Y%m%d)
```

---

## 2. DROP the Temporal keyspaces

> **Irreversible.** The Cassandra keyspaces own all workflow event
> history; once dropped, every in-flight Temporal execution is gone.
> The pre-flight in §1 must be green first.

### 2.1 Connect with cqlsh

```bash
# Target a healthy Cassandra pod in the local DC. The exact pod
# name depends on the cluster name and rack.
POD=$(kubectl -n cassandra get pod -l cassandra.datastax.com/cluster=of-cass-prod \
  -o jsonpath='{.items[0].metadata.name}')

kubectl -n cassandra exec -it "$POD" -- cqlsh
```

### 2.2 Verify the keyspaces exist before the drop

```cql
DESC KEYSPACES;
-- Expect to see `temporal_persistence` and `temporal_visibility`
-- alongside the OpenFoundry application keyspaces.
```

If the keyspaces are not present (e.g. dev cluster that never
booted Temporal), skip §2.3 — the cleanup is already done.

### 2.3 Drop both keyspaces

```cql
DROP KEYSPACE IF EXISTS temporal_persistence;
DROP KEYSPACE IF EXISTS temporal_visibility;
```

### 2.4 Verify the drop

```cql
DESC KEYSPACES;
-- The two `temporal_*` keyspaces must no longer appear.

EXIT;
```

### 2.5 Replicate across data centres (only if multi-DC)

`DROP KEYSPACE` writes through the schema-disagreement protocol and
propagates automatically once the surviving DCs converge. Confirm:

```bash
kubectl -n cassandra exec "$POD" -- nodetool describecluster | head -20
# `Schema versions:` block must show ONE schema version across every
# host. A second version means the schema change is still propagating;
# wait one minute and re-check.
```

If the disagreement persists past 5 minutes, force a repair on the
system_schema keyspace from one node:

```bash
kubectl -n cassandra exec "$POD" -- nodetool repair system_schema
```

---

## 3. Post-cleanup checklist

* [ ] `helmfile -e {dev,staging,prod} list` produces no `temporal` row.
* [ ] `kubectl get ns | grep temporal` is empty.
* [ ] `cqlsh -e 'DESC KEYSPACES;'` does not list `temporal_*`.
* [ ] `nodetool describecluster` shows a single schema version.
* [ ] No `temporal-cassandra-credentials` Secret remains in the cluster:
  `kubectl get secret --all-namespaces | grep temporal` (delete any
  leftover Secrets / ConfigMaps manually).
* [ ] Vault: revoke `secret/data/cassandra/temporal-user` (legacy
  credentials).

---

## 4. What stays

The Cassandra cluster itself is **not** part of the Temporal
retirement. It remains the substrate for the ontology object store
(`ontology_objects.*` keyspace) and for the application keyspaces
listed in `infra/runbooks/cassandra.md`. Keep that runbook as the
authoritative Cassandra operations doc.

The OpenFoundry workloads that previously talked to Temporal — every
service, plus the new CronJobs — are documented in their own
service READMEs:

* `services/pipeline-build-service/`
* `services/reindex-coordinator-service/`
* `services/workflow-automation-service/`
* `services/automation-operations-service/`
* `services/approvals-service/` (plus the
  `infra/helm/apps/of-platform/templates/approvals-timeout-sweep-cronjob.yaml`
  CronJob).

---

## 5. Failure modes

* **DROP runs but the cluster is in disagreement** — wait. If still
  disagreeing after `nodetool repair system_schema`, page the
  Cassandra on-call (cassandra.md §"schema disagreement").
* **Pre-flight finds a pod called `*temporal*`** — STOP. Trace which
  workload still has it. Most likely a stale Deployment / ReplicaSet
  somewhere; `kubectl get all -A | grep temporal` finds it. Resolve
  before dropping the keyspaces.
* **Pre-flight finds a `temporal-client` Cargo dep** — STOP. FASE 8
  was supposed to retire it. Re-run the FASE 8 checklist.
* **Pre-flight finds a `temporal_*` keyspace but no Medusa snapshot
  budget** — proceed. The whole point of the retirement is that the
  workflow history is no longer business-critical (every domain has
  its own state machine + audit ledger now). Document the decision
  in the cutover ticket.
