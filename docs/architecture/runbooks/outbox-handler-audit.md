# S4.1.a — Outbox Handler Audit

> Stream: S4 Outbox / Debezium  
> Owner: Platform / data-plane maintainers  
> Status: executable checklist + handler evidence updated on 2026-05-03

This runbook is the source of truth for whether a **state-mutating
handler** publishes a transactional outbox event that Debezium can route
to Kafka.

## Closure checklist

- [x] Every **critical active handler** listed in the evidence table below
  emits through `outbox::enqueue` in the same transaction as the primary
  write.
- [x] `event-streaming-service` publishes `dataset.streaming.changed.v1`
  for stream/branch/window/topology lifecycle mutations.
- [x] `connector-management-service` publishes
  `connector.connection.changed.v1` for connection lifecycle mutations.
- [x] The outbox substrate exists in every source database that now emits:
  `outbox.events` + `outbox.heartbeat`.
- [x] Debezium connectors exist for every emitting Postgres cluster:
  `pg-policy`, `pg-schemas`, `pg-runtime-config`.
- [x] Kafka topics and Debezium write ACLs exist for every emitted topic.
- [x] Apicurio contract artifacts exist for every emitted topic.
- [x] Contract tests cover topic, key, deterministic `event_id`,
  payload/schema validity, and basic ordering/idempotence semantics.

## Rules

1. **Same transaction, same database.**  
   The outbox write must happen in the same SQL transaction as the
   primary mutation. If the primary write is in Postgres, the outbox
   lives in that same Postgres cluster. If the primary write is outside
   Postgres (for example ontology objects in Cassandra), the handler may
   use a dedicated Postgres sidecar transaction only when the write path
   already models retries/idempotence explicitly, as
   [`apply_object_with_outbox`](../../../libs/ontology-kernel/src/domain/writeback.rs)
   does today.

2. **Topic naming.**  
   `<domain>.<entity>.<event>.v<N>`. New schema version means a new
   topic, never a silent break.

3. **Deterministic `event_id`.**  
   Use UUID v5 derived from `aggregate + aggregate_id + version_token`.
   Retries with the same logical mutation must converge on the same
   outbox primary key.

4. **Connector scope.**  
   Debezium connectors read only `outbox.events` from each consolidated
   cluster. They do not sniff business tables directly.

5. **What counts as critical.**  
   A handler is in scope if it mutates control-plane state that another
   service, UI, indexer, or downstream materializer must observe through
   Kafka. Internal-only retries/counters/ephemeral runtime state are out
   of scope unless another bounded context consumes them.

## Source cluster / connector matrix

| Primary write lives in | Outbox connector | Current emitted topics |
| --- | --- | --- |
| `pg-policy` | [`outbox-pg-policy`](../../../infra/k8s/platform/manifests/debezium/kafka-connector-outbox-pg-policy.yaml) | `ontology.object.changed.v1`, `ontology.action.applied.v1`, `audit.events.v1`, `lineage.events.v1` |
| `pg-schemas` | [`outbox-pg-schemas`](../../../infra/k8s/platform/manifests/debezium/kafka-connector-outbox-pg-schemas.yaml) | `dataset.streaming.changed.v1` |
| `pg-runtime-config` | [`outbox-pg-runtime-config`](../../../infra/k8s/platform/manifests/debezium/kafka-connector-outbox-pg-runtime-config.yaml) | `connector.connection.changed.v1` |

## Handler evidence

| Domain | Handler(s) | Mutation kind | Topic | Evidence | Status |
| --- | --- | --- | --- | --- | --- |
| ontology | [`apply_object_with_outbox`](../../../libs/ontology-kernel/src/domain/writeback.rs) via [`handlers::objects`](../../../libs/ontology-kernel/src/handlers/objects.rs) and [`handlers::actions`](../../../libs/ontology-kernel/src/handlers/actions.rs) | object upsert / object patch | `ontology.object.changed.v1` | Existing transactional helper + [`services/ontology-actions-service/tests/writeback_e2e.rs`](../../../services/ontology-actions-service/tests/writeback_e2e.rs) | ✅ |
| event streaming | [`streams::create_stream`](../../../services/event-streaming-service/src/handlers/streams.rs), [`streams::update_stream`](../../../services/event-streaming-service/src/handlers/streams.rs) | stream definition create/update | `dataset.streaming.changed.v1` | Handler begins SQL tx, writes `streaming_streams`, seeds schema history in the same tx, then calls [`event_streaming_service::outbox::emit`](../../../services/event-streaming-service/src/outbox.rs). Contract tests in the same module validate topic/key/schema/idempotent `event_id`. | ✅ |
| event streaming | [`branches::create_branch`](../../../services/event-streaming-service/src/handlers/branches.rs), [`delete_branch`](../../../services/event-streaming-service/src/handlers/branches.rs), [`merge_branch`](../../../services/event-streaming-service/src/handlers/branches.rs), [`archive_branch`](../../../services/event-streaming-service/src/handlers/branches.rs) | branch lifecycle | `dataset.streaming.changed.v1` | Branch rows mutate inside one SQL tx and enqueue a branch event before commit. Merge/archive payloads pin `target_branch_id`, `merged_sequence_no`, `archived_at`. Tests: [`services/event-streaming-service/src/outbox.rs`](../../../services/event-streaming-service/src/outbox.rs). | ✅ |
| event streaming | [`streams::create_window`](../../../services/event-streaming-service/src/handlers/streams.rs), [`streams::update_window`](../../../services/event-streaming-service/src/handlers/streams.rs) | window definition create/update | `dataset.streaming.changed.v1` | Window create/update now run inside SQL tx and emit before commit. Tests assert distinct version tokens for created vs updated events and schema-valid payloads. | ✅ |
| event streaming | [`topologies::create_topology`](../../../services/event-streaming-service/src/handlers/topologies.rs), [`topologies::update_topology`](../../../services/event-streaming-service/src/handlers/topologies.rs) | topology definition create/update | `dataset.streaming.changed.v1` | Topology handlers validate config, persist, reload row inside tx, and enqueue before commit. Tests validate schema and deterministic keying. | ✅ |
| connector management | [`connections::create_connection`](../../../services/connector-management-service/src/handlers/connections.rs), [`test_connection`](../../../services/connector-management-service/src/handlers/connections.rs), [`delete_connection`](../../../services/connector-management-service/src/handlers/connections.rs) | connection lifecycle / status | `connector.connection.changed.v1` | Handlers now wrap `INSERT`, status `UPDATE`, and `DELETE` in SQL tx and enqueue through [`connector_management_service::outbox`](../../../services/connector-management-service/src/outbox.rs). Tests validate topic/key/schema/idempotent `event_id`. | ✅ |

## Explicitly out of scope for this cut

These handlers still mutate state, but they do **not** currently
require a downstream Kafka contract, so they are not blocking Debezium
unpause:

- `event-streaming-service::push_events`
- `event-streaming-service::replay_dead_letter`
- `connector-management-service::registrations::*`
- `connector-management-service::data_connection::*`

If any of them gains a downstream consumer, add a new row here before
shipping it.

## Infra evidence

- Outbox DDL now exists in:
  [`libs/outbox/migrations/0001_outbox_events.sql`](../../../libs/outbox/migrations/0001_outbox_events.sql),
  [`services/event-streaming-service/migrations/20260503010000_outbox.sql`](../../../services/event-streaming-service/migrations/20260503010000_outbox.sql),
  [`services/connector-management-service/migrations/20260503010000_outbox.sql`](../../../services/connector-management-service/migrations/20260503010000_outbox.sql).
- Consolidated cluster bootstrap now provisions shared `outbox` access
  and Debezium roles in:
  [`pg-policy-bootstrap-sql.yaml`](../../../infra/k8s/platform/manifests/cnpg/clusters/pg-policy-bootstrap-sql.yaml),
  [`pg-schemas-bootstrap-sql.yaml`](../../../infra/k8s/platform/manifests/cnpg/clusters/pg-schemas-bootstrap-sql.yaml),
  [`pg-runtime-config-bootstrap-sql.yaml`](../../../infra/k8s/platform/manifests/cnpg/clusters/pg-runtime-config-bootstrap-sql.yaml).
- Logical decoding is enabled on:
  [`pg-policy.yaml`](../../../infra/k8s/platform/manifests/cnpg/clusters/pg-policy.yaml),
  [`pg-schemas.yaml`](../../../infra/k8s/platform/manifests/cnpg/clusters/pg-schemas.yaml),
  [`pg-runtime-config.yaml`](../../../infra/k8s/platform/manifests/cnpg/clusters/pg-runtime-config.yaml).
- Topic + ACL + schema registration evidence:
  [`topics-domain-v1.yaml`](../../../infra/k8s/platform/manifests/strimzi/topics-domain-v1.yaml),
  [`kafka-user-debezium-connect.yaml`](../../../infra/k8s/platform/manifests/debezium/kafka-user-debezium-connect.yaml),
  [`infra/docker-compose.yml`](../../../infra/docker-compose.yml).

## Pre-unpause / rollout gate

Debezium is no longer paused for **coverage** reasons in the active
handler set above. Before production rollout, still verify:

1. connector credentials for `pg-policy`, `pg-schemas`, and
   `pg-runtime-config` exist in the `kafka` namespace;
2. the three connectors report `RUNNING`;
3. the DLQ `__dlq.outbox-pg-policy.v1` stays empty during a smoke test;
4. the chaos test in
   [`infra/k8s/platform/manifests/debezium/chaos-test.md`](../../../infra/k8s/platform/manifests/debezium/chaos-test.md)
   is signed off.
