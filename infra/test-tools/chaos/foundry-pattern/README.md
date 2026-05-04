# Foundry-pattern resilience suite

> Companion to FASE 11 / Tarea 11.3 of
> [`migration-plan-foundry-pattern-orchestration.md`](../../../../docs/architecture/migration-plan-foundry-pattern-orchestration.md)
> and to ADR-0037.

The four standing Schedules in this directory replace the retired
`temporal-history-kill.yaml` (deleted in FASE 9) and validate that the
Foundry-pattern substrate has no single point of failure.

## Files

| File                                   | Target                            | Schedule (UTC) | Success criterion                                                         |
|----------------------------------------|-----------------------------------|----------------|---------------------------------------------------------------------------|
| `workflow-automation-kill.yaml`        | one workflow-automation-service Pod | Mon 03:00      | smoke `foundry-pattern-full-flow.json` passes within 90 s                 |
| `automation-operations-kill.yaml`      | one automation-operations-service Pod | Tue 03:00    | in-flight cleanup_workspace saga reaches a terminal state within 120 s    |
| `debezium-connect-kill.yaml`           | one Debezium Connect worker Pod   | Wed 03:00      | `debezium_metrics_milli_seconds_behind_source` < 5 s within 60 s          |
| `spark-operator-kill.yaml`             | Spark Operator controller Pod     | Thu 03:00      | in-flight pipeline run reaches `Succeeded` within its existing timeout    |

## Apply (staging only)

```sh
kubectl apply -f infra/test-tools/chaos/foundry-pattern/
```

## DO NOT install in production

These manifests assume the cluster is non-revenue. Production resilience
is verified by the standing weekly DR drill (see
`infra/runbooks/dr-failover.md`), not by Chaos Mesh schedules.

## Pause / resume

```sh
kubectl annotate schedule -n chaos-mesh chaos-fp-workflow-automation-kill \
    chaos-mesh.org/pause=true --overwrite
```

## How this differs from the parent suite

The four manifests in [`../`](../) cover the *operational stores*
(Cassandra, Kafka, Postgres via the node-drain) — the substrate the
data plane writes to. The four here cover the *orchestration plane*
(consumers, runners, CDC, Spark operator) — the substrate that
replaced Temporal. Both suites are independent and can run in parallel.
