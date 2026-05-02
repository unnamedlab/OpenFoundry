# ADR-0032 — Chaos Mesh resilience suite

| Field | Value |
| --- | --- |
| Status | Accepted |
| Date | 2026-05-02 |
| Stream | S8.4 (cleanup & hardening) |
| Related | [ADR-0020](ADR-0020-cassandra-as-operational-store.md), [ADR-0024](ADR-0024-postgres-consolidation.md), [ADR-0030](ADR-0030-service-consolidation-30-targets.md), [ADR-0031](ADR-0031-helm-chart-split-five-releases.md) |

## Context

Stream S7 added a second region with active/standby replication for
Postgres, Cassandra, Kafka and the Iceberg lakehouse. Stream S8.1
consolidated 97 service crates to 30 (larger blast radius per Pod).
Both changes need standing failure-injection tests to prove the SLOs
(P95 reads ≤ 25 ms, write durability ≤ 60 s RPO, RTO ≤ 30 min) hold
under realistic failures.

We ran ad-hoc kill-Pod drills during S7.5, but a one-off DR game day
doesn't catch regressions introduced by routine PRs (e.g. a
NetworkPolicy change that breaks Cassandra gossip after the next
reboot).

## Decision

Adopt **Chaos Mesh** as the resilience-testing platform and run a
fixed set of four standing experiments on a monthly schedule against
the staging cluster.

### Tooling

* **Chaos Mesh** (Apache 2.0), installed via its official Helm chart
  in the `chaos-mesh` namespace.
* Manifests stored in [`infra/k8s/chaos/`](../../../infra/k8s/chaos/).
* Experiments deployed as `Schedule` resources — Chaos Mesh manages
  the cron, ramp-up and rollback.
* Observability: each experiment is annotated so Grafana renders an
  overlay band on the SLO dashboards; alerts during a chaos window
  are tagged `chaos=true` and routed to a separate channel.

### Standing experiments

1. **Cassandra Pod kill** —
   [`cassandra-kill.yaml`](../../../infra/k8s/chaos/cassandra-kill.yaml).
   Kills one Cassandra Pod every Tuesday 02:00 UTC. Success: P95
   read ≤ 25 ms within 60 s of kill, no client error budget burn.
2. **Kafka broker kill** —
   [`kafka-broker-kill.yaml`](../../../infra/k8s/chaos/kafka-broker-kill.yaml).
   Kills one Strimzi broker Pod every Wednesday 02:00 UTC. Success:
   consumer lag returns to baseline within 90 s, no committed-offset
   loss.
3. **Temporal history Pod kill** —
   [`temporal-history-kill.yaml`](../../../infra/k8s/chaos/temporal-history-kill.yaml).
   Kills one Temporal history Pod every Thursday 02:00 UTC. Success:
   in-flight workflows continue (no `WorkflowExecutionTimedOut`
   metric increment).
4. **k8s node drain** —
   [`k8s-node-drain.yaml`](../../../infra/k8s/chaos/k8s-node-drain.yaml).
   Drains one worker node every Friday 02:00 UTC. Success: PDBs
   honoured (no service drops below `minAvailable`), all Pods
   reschedule within 5 min.

### Cadence

* **Staging**: monthly schedule (above).
* **Production**: opt-in only. Chaos Mesh is installed but no
  Schedules are active. Manual one-shot runs allowed during
  quarterly DR game days (see
  [`infra/runbooks/dr-game-day.md`](../../../infra/runbooks/dr-game-day.md)).

### Gating

A failed experiment in staging:

1. Auto-pauses the Schedule (Chaos Mesh `paused: true`).
2. Files an issue tagged `chaos-failed` against the owning team
   (label derived from the `app.kubernetes.io/part-of` of the killed
   Pod).
3. Blocks the next Helm release of the affected sub-chart until the
   issue is closed.

## Consequences

### Positive

* Continuous, observable proof that the SLOs hold under single-node
  failure.
* Catches regressions (NetworkPolicy, RBAC, PDB) introduced by
  routine PRs before they reach prod.
* Forms the seed of the production runbook: every chaos failure
  becomes a documented mitigation.

### Negative

* Adds a recurring maintenance window in staging. Mitigated by
  running outside business hours and tagging metrics so SLO
  dashboards exclude the window when computing month-over-month
  trends.
* Chaos Mesh itself is one more operator to maintain. Acceptable —
  the alternatives (Litmus, custom scripts) would either lock us
  in deeper or push the maintenance into bash.

## References

* Chaos Mesh — <https://chaos-mesh.org>
* [ADR-0020 — Cassandra as operational store](ADR-0020-cassandra-as-operational-store.md)
* [`infra/runbooks/dr-game-day.md`](../../../infra/runbooks/dr-game-day.md)
* [`infra/k8s/chaos/`](../../../infra/k8s/chaos/)
