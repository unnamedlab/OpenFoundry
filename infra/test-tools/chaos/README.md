# Chaos Mesh resilience suite

> Companion to [ADR-0032](../../../docs/architecture/adr/ADR-0032-chaos-mesh-resilience-suite.md).
> Standing failure-injection experiments executed monthly in
> staging.

## Install (one-time)

```sh
helm repo add chaos-mesh https://charts.chaos-mesh.org
helm upgrade --install chaos-mesh chaos-mesh/chaos-mesh \
    --namespace chaos-mesh --create-namespace \
    --version 2.6.3 \
    --set chaosDaemon.runtime=containerd \
    --set chaosDaemon.socketPath=/run/containerd/containerd.sock
```

## Apply the standing schedules (staging only)

```sh
kubectl apply -f infra/k8s/chaos/
```

## Files

| File | Target | Schedule (UTC) | Success criterion |
| ---- | ------ | -------------- | ----------------- |
| [`cassandra-kill.yaml`](cassandra-kill.yaml) | one Cassandra Pod (`dc1`) | Tue 02:00 | P95 read ≤ 25 ms within 60 s |
| [`kafka-broker-kill.yaml`](kafka-broker-kill.yaml) | one Strimzi broker | Wed 02:00 | consumer lag back to baseline ≤ 90 s |
| [`temporal-history-kill.yaml`](temporal-history-kill.yaml) | one Temporal history Pod | Thu 02:00 | no `WorkflowExecutionTimedOut` |
| [`k8s-node-drain.yaml`](k8s-node-drain.yaml) | one worker node | Fri 02:00 | PDBs honoured, reschedule ≤ 5 min |

## Pause / resume

```sh
kubectl annotate schedule -n chaos-mesh chaos-cassandra-kill \
    chaos-mesh.org/pause=true --overwrite
```

## DO NOT install in production

The Schedules in this directory are scoped to the staging cluster.
Production keeps Chaos Mesh installed but with **no** Schedule
applied; manual one-shot experiments require sign-off (see
[`../../runbooks/dr-game-day.md`](../../runbooks/dr-game-day.md)).
