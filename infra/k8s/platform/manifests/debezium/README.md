# Debezium Kafka Connect — `outbox-pg-policy`

> Stream: S4 · Tareas S4.1.b–g
> License: Apache-2.0 (Strimzi + Debezium upstream Apache-2.0
> connectors). No Confluent Platform.

This directory ships the **desired state** for the Debezium Postgres
connectors that drain the transactional outbox table
`outbox.events` (owned by `libs/outbox`) into Kafka from the
consolidated Postgres clusters that host outbox-enabled services.

## Files

| File | Purpose |
|------|---------|
| `kafka-connect.yaml` | Strimzi `KafkaConnect` CR — 2-replica Connect cluster running the Debezium Postgres image with the Apicurio Avro converter pre-built. |
| `kafka-connector-outbox-pg-policy.yaml` | Strimzi `KafkaConnector` CR — outbox source for `pg-policy`. |
| `kafka-connector-outbox-pg-schemas.yaml` | Strimzi `KafkaConnector` CR — outbox source for `pg-schemas`. |
| `kafka-connector-outbox-pg-runtime-config.yaml` | Strimzi `KafkaConnector` CR — outbox source for `pg-runtime-config`. |
| `kafka-user-debezium-connect.yaml` | `KafkaUser` (TLS auth) + ACLs Debezium needs to publish to `<domain>.*.v1` topics and to manage its own internal Connect topics. |
| `pod-monitor.yaml` | `PodMonitor` for the Connect pods so Prometheus can scrape `/metrics` (KafkaConnect Prometheus JMX exporter). |
| `prometheus-rules.yaml` | `PrometheusRule` with the two gate alerts: replication-slot lag > 100 MB and Connect tasks not `RUNNING`. |
| `chaos-test.md` | S4.1.g chaos runbook: kill a Connect pod, verify no dup / loss. |

## Apply order

```bash
# Pre-req: pg-policy CNPG cluster exists with `wal_level=logical`
# (set by S6.1.b). Until then this connector stays paused.

kubectl apply -f infra/k8s/platform/manifests/debezium/kafka-user-debezium-connect.yaml
kubectl apply -f infra/k8s/platform/manifests/debezium/kafka-connect.yaml
kubectl -n kafka wait --for=condition=Ready kafkaconnect/debezium --timeout=15m

kubectl apply -f infra/k8s/platform/manifests/debezium/kafka-connector-outbox-pg-policy.yaml
kubectl apply -f infra/k8s/platform/manifests/debezium/kafka-connector-outbox-pg-schemas.yaml
kubectl apply -f infra/k8s/platform/manifests/debezium/kafka-connector-outbox-pg-runtime-config.yaml

kubectl apply -f infra/k8s/platform/manifests/debezium/pod-monitor.yaml
kubectl apply -f infra/k8s/platform/manifests/debezium/prometheus-rules.yaml
```

## Connector contract

* **Source:** one logical replication slot per consolidated cluster
  (`debezium_outbox_pg_policy`,
  `debezium_outbox_pg_schemas`,
  `debezium_outbox_pg_runtime_config`) against schema `outbox`,
  table `events`.
* **Routing SMT:** `io.debezium.transforms.outbox.EventRouter`.
  Routes on the `topic` column (matches the `OutboxEvent::topic`
  field). `route.by.field=topic` and
  `route.topic.replacement=${routedByValue}`.
* **Cleanup contract:** the source row is **already deleted** by
  the application transaction (`enqueue` does INSERT+DELETE in the
  same tx — see [`libs/outbox/src/lib.rs`](../../../../../libs/outbox/src/lib.rs)).
  The connector therefore runs with `tombstones.on.delete=false`
  and the table stays empty in steady state.
  This is the substrate that maps to the plan's
  `outbox.event.deletion.policy=delete` requirement
  (the plan's option name is shorthand; Debezium upstream does not
  expose that exact key).
* **Schemas:** `value.converter=io.apicurio.registry.utils.converter.AvroConverter`
  with `value.converter.apicurio.registry.url=http://apicurio-registry.apicurio:8080/apis/registry/v2`.
* **Idempotency:** producer uses `acks=all`,
  `enable.idempotence=true`,
  `max.in.flight.requests.per.connection=5`. Combined with the
  deterministic `event_id` from the application, a Connect-task
  restart cannot publish duplicates.

## Topics produced

`<domain>.<entity>.<event>.v<N>` — see
[`infra/k8s/platform/manifests/strimzi/topics-domain-v1.yaml`](../strimzi/topics-domain-v1.yaml)
for the four canonical topics the plan calls out. Adding a new
versioned topic is purely additive: just register the schema in
Apicurio and add a `KafkaTopic` CR.

## Production gate

Connector rollout is gated on:

1. [`docs/architecture/runbooks/outbox-handler-audit.md`](../../../../../docs/architecture/runbooks/outbox-handler-audit.md) — every critical active handler in scope wired and evidenced.
2. `pg-policy`, `pg-schemas` and `pg-runtime-config` running with
   `wal_level=logical` and replication-slot headroom.
3. Schemas registered in Apicurio for every emitted topic
   (`ontology.object.changed.v1`, `ontology.action.applied.v1`,
   `dataset.streaming.changed.v1`,
   `connector.connection.changed.v1`,
   `audit.events.v1`, `lineage.events.v1`).
4. Chaos test ([`chaos-test.md`](chaos-test.md)) signed off.
