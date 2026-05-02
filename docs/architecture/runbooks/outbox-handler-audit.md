# S4.1.a — Outbox handler audit

> Stream: S4 Outbox/Debezium · Tarea S4.1.a
> Owner: Platform / data-plane maintainers
> Status: substrate inventory; per-service wiring tracked as
> follow-up PRs.

This document audits every **mutation handler** that must publish a
domain event to Kafka, and pins whether it currently uses
[`outbox::enqueue`](../../../libs/outbox/src/lib.rs) inside its
Postgres transaction. Anything *not* wired is a follow-up before
S4.1.b can flip Debezium on for that schema.

The contract is non-negotiable:

> Every state change that downstream services or the indexer must see
> **must** call `outbox::enqueue` inside the same `sqlx::Transaction`
> as the primary write. No "fire-and-forget" Kafka producers.

The pattern is documented in
[`libs/outbox/README.md`](../../../libs/outbox/README.md) and
implemented as INSERT+DELETE in the same transaction (canonical
Debezium outbox).

## Audit table

| Domain | Handler | Mutation kind | Wired? | Topic | Follow-up |
|--------|---------|---------------|--------|-------|-----------|
| **ontology** | [`libs/ontology-kernel::domain::writeback::apply_object_with_outbox`](../../../libs/ontology-kernel/src/domain/writeback.rs) | object upsert | ✅ | `ontology.object.changed.v1` | — |
| ontology | `ontology-actions-service` action apply | action effect | ⚠️ partial | `ontology.action.applied.v1` | wire kernel handler `actions::apply` to `enqueue` |
| ontology | `ontology-actions-service` link writeback | link upsert | ⚠️ partial | `ontology.link.changed.v1` | extend `apply_object_with_outbox` to links |
| **identity** | `identity-federation-service::handlers::user_mgmt` (deactivate) | user mutation | ❌ | `identity.user.changed.v1` | wire enqueue |
| identity | `identity-federation-service::handlers::role_mgmt` | role+permission upsert | ❌ | `identity.role.changed.v1` | wire enqueue |
| identity | `identity-federation-service::handlers::mfa` (delete TOTP) | mfa state | ❌ | `audit.identity.v1` (already covered by S3.1.g audit envelope) | dedup with audit-topic substrate |
| identity | `authorization-policy-service::handlers::{role_mgmt, group_mgmt, policy_mgmt, restricted_views}` | policy CRUD | ❌ | `policy.role.changed.v1`, `policy.group.changed.v1`, `policy.abac.changed.v1`, `policy.restricted_view.changed.v1` | wire enqueue per file |
| **datasets** | `dataset-versioning-service::handlers::lifecycle` (create/freeze) | dataset lifecycle | ❌ | `dataset.lifecycle.changed.v1` | wire enqueue |
| datasets | `dataset-versioning-service::handlers::foundry` (view files / branch fallback DELETEs) | dataset writeback | ❌ | `dataset.view.changed.v1`, `dataset.branch.changed.v1` | wire enqueue |
| datasets | `data-asset-catalog-service::handlers::{crud, upload, views}` | dataset metadata | ❌ | `dataset.catalog.changed.v1` | wire enqueue |
| datasets | `dataset-quality-service::handlers::quality` (rule upsert/delete) | quality rules | ❌ | `dataset.quality.changed.v1` | wire enqueue |
| datasets | `data-connectivity` `connector-management-service::handlers::connections` | connector lifecycle | ❌ | `connector.connection.changed.v1` | wire enqueue |
| datasets | `entity-resolution-service` job state (`fusion_clusters` DELETE) | ER job lifecycle | ❌ | `entity_resolution.job.changed.v1` | wire enqueue |
| **lineage** | `event-streaming-service::handlers::branches` | stream branch CRUD | ❌ | `lineage.events.v1` (legacy) → migrate to `dataset.streaming.changed.v1` | wire enqueue + retire legacy topic |
| **product/apps** | `app-builder-service::handlers::apps` (delete) | app lifecycle | ❌ | `apps.app.changed.v1` | wire enqueue |
| product/apps | `marketplace-service::handlers::install` (install_count) | install metric | ⚠️ low-signal | — | acceptable to keep direct UPDATE; emit only on major lifecycle events |
| product/apps | `notebook-runtime-service::handlers::{crud, execute, notepad}` | notebook lifecycle | ❌ | `notebook.notebook.changed.v1` | wire enqueue |
| product/apps | `document-reporting-service::handlers::notepad` | doc lifecycle | ❌ | `documents.notepad.changed.v1` | wire enqueue |
| **operations** | `pipeline-authoring-service::handlers::crud` (delete pipeline) | pipeline definition | ❌ | `pipeline.definition.changed.v1` | wire enqueue |
| operations | `workflow-automation-service::handlers::crud` (delete workflow) | workflow definition | ❌ | `workflow.definition.changed.v1` | wire enqueue (overlaps with Temporal — emit only definition, not runs) |
| operations | `tenancy-organizations-service::handlers::{trash, projects}` | project lifecycle | ❌ | `tenancy.project.changed.v1` | wire enqueue |
| operations | `retention-policy-service::handlers::retention` | retention policy CRUD | ❌ | `policy.retention.changed.v1` | wire enqueue |

> **Excluded by design:**
>
> * Anything that already has a dedicated Kafka path via Temporal
>   activities (S2.x audit, ai-events). These flow through the worker,
>   not through the handler outbox.
> * Soft-delete-only updates inside an ER job that are completely
>   contained in the job lifecycle (the job-level event covers them).
> * Sessions / refresh tokens / OAuth state — these live in
>   Cassandra (S3) and emit through `audit.identity.v1`.

## Conventions for follow-up PRs

1. **Topic naming:** `<domain>.<entity>.<event>.v<N>`. Pin the `vN`
   suffix from day one even if there is only `v1`. New schema
   versions are new topics, never silent breaking changes.
2. **`event_id`:** v5 UUID derived from
   `aggregate || aggregate_id || version`. Idempotent retries from
   the handler must converge on the same row.
3. **OpenLineage headers:** if a lineage `run_id` is in scope on the
   request, attach `ol-run-id`, `ol-parent-run-id`, `ol-namespace`
   and `ol-job` via `OutboxEvent::with_header`. Indexer and
   audit-sink rely on these.
4. **Pool wiring:** handlers must use the **`pg-policy`** pool (the
   one that owns `outbox.events`). If the service writes business
   data to a different cluster (`pg-schemas`), it must take a
   distributed transaction → not allowed; instead the handler
   chooses one cluster per logical aggregate per ADR-0022.
5. **Tests:** every wired handler must add a sqlx test that asserts
   one row hits `outbox.events` *during* the transaction (visible to
   `SELECT … FROM outbox.events FOR UPDATE` issued by the same tx),
   regardless of commit outcome.

## Pre-cutover gate (S4.1.b)

Debezium Connect must not be enabled in production until:

1. Every handler in the **❌** rows above has shipped a PR with the
   wiring + tests.
2. The 4 versioned topics in [S4.2.a](../../../infra/k8s/strimzi/topics-domain-v1.yaml)
   are `Ready`.
3. The Apicurio schema for each topic is registered (S4.1.e).
4. Chaos test (S4.1.g) has been signed off.

Until then the connector exists in the cluster but is paused
(`spec.pause: true`), per
[`infra/k8s/debezium/README.md`](../../../infra/k8s/debezium/README.md).
