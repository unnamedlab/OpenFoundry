# Foundry Streaming and Data Connection 1:1 parity checklist

Date: 2026-05-11
Scope: public-docs-based parity plan for OpenFoundry's advanced Data Connection
and streaming surfaces: sources, connectors/source types, workers, agents,
network egress, credentials, source permissions, exploration, batch syncs,
file-based sync modes, table syncs, media sync handoffs, streaming syncs,
push-based stream ingestion, streams, checkpoints, CDC syncs, virtual tables,
virtual media handoffs, exports, streaming exports, table exports, webhooks,
connections from code, external transforms, source imports in Python, compute
module alternatives, operational history, health, retry behavior, and governance
controls for exporting Foundry data into external systems.

This document is intentionally implementation-oriented. It does not attempt to
clone Palantir branding, private source code, proprietary assets, screenshots,
or any non-public behavior. The target is **functional parity based on public
Palantir Foundry documentation**: the same product concepts, comparable
connection authoring and operator workflows, compatible resource models where
useful, and OpenFoundry-native implementation details that can be tested
locally.

## Parity scope boundary

All checklist work is governed by the
[Foundry public-docs parity policy](../reference/foundry-public-docs-parity-policy.md).
OpenFoundry may implement behavior described in public Palantir documentation,
but contributors must not copy private source, decompile bundles, import
tenant-specific exports, use Palantir branding, or reuse proprietary assets.
The product target is functional parity in an OpenFoundry-native implementation,
not a pixel-perfect clone.

This checklist covers Data Connection capabilities that go beyond the webhook
and lightweight REST source surface already tracked by the Workshop/Pipeline
checklist. It should integrate with the Data Foundation checklist for datasets,
branches, transactions, views, builds, schedules, retention, and Data Health;
with the Media Sets checklist for media-specific semantics; with the Ontology
checklist for object indexing over streams and CDC; and with the security
checklist for markings, checkpoints, export control, and sensitive-data policy.

## Status vocabulary

| Status | Meaning |
| --- | --- |
| `todo` | Not implemented or not yet verified in OpenFoundry. |
| `partial` | Some surface exists, but behavior is incomplete or not wired end-to-end. |
| `blocked` | Requires a platform dependency, public documentation, or product decision. |
| `done` | Implemented, tested, documented, and verified through UI or API smoke tests. |

## Priority vocabulary

| Priority | Meaning |
| --- | --- |
| `P0` | Required for credible ingestion/export workflows used by the Trail Running demo and basic production data movement. |
| `P1` | Required for Foundry-style Data Connection and streaming parity beyond simple REST/webhook demos. |
| `P2` | Advanced, governance-heavy, connector-specific, or scale-oriented parity. |

## Official Palantir documentation library

These public docs should be treated as the external behavioral contract while
implementing this checklist.

### Product, Data Integration, and Data Connection overview

- [Data integration overview](https://www.palantir.com/docs/foundry/data-integration/overview/)
- [Connecting to data](https://www.palantir.com/docs/foundry/data-integration/connecting-to-data/)
- [Source type overview](https://www.palantir.com/docs/foundry/data-integration/source-type-overview/)
- [Data Connection overview](https://www.palantir.com/docs/foundry/data-connection/overview)
- [Data Connection core concepts](https://www.palantir.com/docs/foundry/data-connection/core-concepts/)

### Source setup, networking, agents, and permissions

- [Initial setup overview](https://www.palantir.com/docs/foundry/data-connection/initial-setup-overview/)
- [Set up a source](https://www.palantir.com/docs/foundry/data-connection/set-up-source)
- [Configure egress](https://www.palantir.com/docs/foundry/administration/configure-egress/)
- [Agent proxy configuration reference](https://www.palantir.com/docs/foundry/data-connection/agent-proxy/)
- [Available connectors: other source types](https://www.palantir.com/docs/foundry/available-connectors/other-source-types/)

### Syncs, streams, CDC, and virtualized data

- [Set up a sync](https://www.palantir.com/docs/foundry/data-connection/set-up-sync)
- [File-based syncs](https://www.palantir.com/docs/foundry/data-connection/file-based-syncs/)
- [Set up a streaming sync](https://www.palantir.com/docs/foundry/data-connection/set-up-streaming-sync/)
- [Streams core concepts](https://www.palantir.com/docs/foundry/data-integration/streams/)
- [Push data into a stream](https://www.palantir.com/docs/foundry/data-connection/push-based-ingestion/)
- [Change data capture](https://www.palantir.com/docs/foundry/data-integration/change-data-capture)
- [Virtual tables](https://www.palantir.com/docs/foundry/data-integration/virtual-tables/)
- [Set up a media set sync](https://www.palantir.com/docs/foundry/data-connection/media-set-sync)

### Exports, webhooks, and connections from code

- [Exports overview](https://www.palantir.com/docs/foundry/data-connection/export-overview/)
- [Webhooks overview](https://www.palantir.com/docs/foundry/data-connection/webhooks-overview)
- [Set up a Webhook](https://www.palantir.com/docs/foundry/data-connection/webhooks-setup/)
- [Webhooks configuration reference](https://www.palantir.com/docs/foundry/data-connection/webhooks-reference)
- [External transforms](https://www.palantir.com/docs/foundry/data-connection/external-transforms)
- [Sources in Python environments](https://www.palantir.com/docs/foundry/data-connection/sources-in-python/)

### Observability and downstream integration

- [Data Health](https://www.palantir.com/docs/foundry/observability/data-health/)
- [Builds core concepts](https://www.palantir.com/docs/foundry/data-integration/builds/)
- [Schedules core concepts](https://www.palantir.com/docs/foundry/data-integration/schedules/)
- [Create incremental syncs](https://www.palantir.com/docs/foundry/building-pipelines/create-incremental-syncs/)

## Target OpenFoundry resource model

The implementation should define stable OpenFoundry-owned resources that can map
to public Foundry concepts without requiring Palantir RID formats. Compatibility
aliases may be accepted at service boundaries, but persisted state should use
OpenFoundry canonical IDs.

| Public Foundry concept | OpenFoundry resource target | Required notes |
| --- | --- | --- |
| Source | `data_source` | One connection to an external system with connector type, worker, network policy, credentials, permissions, capabilities, and default output location. |
| Source type / connector | `connector_type` | Registry entry describing supported capabilities, worker compatibility, credentials schema, network requirements, exploration behavior, and setup form. |
| Worker | `connection_worker` | Runtime placement for connection capabilities: OpenFoundry worker, agent worker, or code-driven alternative. |
| Agent | `connection_agent` | Agent process registration, heartbeat, version, host metadata, reachable network policies, and capability compatibility. |
| Egress policy | `egress_policy` | Network route, allowlist, proxy mode, target host/port, agent proxy behavior, and approval state. |
| Credential | `connection_credential` | Encrypted secret material with schema, rotation metadata, owner, usage references, and audit events. |
| Source permission | `source_permission` | Resource roles for viewing source configuration, editing source, using source, importing source into code, and exporting Foundry data through the source. |
| Capability | `connection_capability` | Batch sync, file sync, table sync, media sync, streaming sync, CDC sync, file export, table export, streaming export, webhook, virtual table, virtual media, and exploration support. |
| Exploration session | `source_exploration` | Browse/test connection session that lists external folders, files, databases, schemas, tables, topics, queues, and sample schemas. |
| Sync | `data_sync` | Configured import from external system into a dataset, stream, or media set with mode, schedule/build integration, schema, history, and output resource. |
| File sync | `file_sync` | Sync specialization for external files with path filters, file limits, already-synced filtering, transformers, and transaction behavior. |
| Streaming sync | `streaming_sync` | Long-running pull from an external stream/topic/queue into an OpenFoundry stream. |
| CDC sync | `cdc_sync` | Changelog sync with primary key, ordering column, deletion column, live/archive views, and downstream changelog metadata. |
| Stream | `stream_dataset` | Tabular stream with hot buffer, cold/archive dataset, schema, branch, permissions, checkpoint state, consistency mode, and replay behavior. |
| Push ingestion endpoint | `stream_ingest_endpoint` | Authenticated REST endpoint for pushing records into streams by dataset/stream ID and branch. |
| Virtual table | `virtual_table` | Foundry-style pointer to an external table with schema, query pushdown metadata, update detection, lineage, permissions, and limitations. |
| Export | `data_export` | Configured push from Foundry dataset or stream to an external destination with type, mode, schedule/start-stop control, history, and governance. |
| Webhook | `connection_webhook` | Request definition on a source with input parameters, request builder, output extraction, history, and action/function integration. |
| Code import | `source_code_import` | Allowlist and generated bindings for using a source from Python transforms, external functions, or compute modules. |
| Connection job | `connection_job` | Run instance for sync/export/exploration/webhook/virtual registration with status, logs, retries, metrics, and build integration. |
| Data health check | `connection_health_check` | Health rule over sources, agents, syncs, streams, exports, virtual tables, and webhooks. |
| Export control policy | `source_export_policy` | Governance configuration describing whether Foundry inputs may be exported, what markings/orgs are exportable, and audit behavior. |

## Milestone A: minimum viable Data Connection and streaming parity

### Data Connection application and source setup

- [x] `SDC.1` Data Connection application shell (`P0`, `done`)
  - Provide Sources, Syncs, Streams, Exports, Webhooks, Virtual Tables, Agents, and Health views.
  - Include global search, capability filters, worker filters, owner filters, status filters, recent failures, and source-type discovery.
  - Show clear entry points for New source, Explore, Create sync, Create export, Create webhook, and Register virtual table.
  - Implemented in `apps/web/src/routes/data-connection/DataConnectionPage.tsx` with OpenFoundry-native shell navigation, filters, source-type discovery cards, recent-failure surfacing, and capability-aware entry points.
  - Docs: [Data Connection overview](https://www.palantir.com/docs/foundry/data-connection/overview), [Data Connection core concepts](https://www.palantir.com/docs/foundry/data-connection/core-concepts/).

- [x] `SDC.2` Connector/source type registry (`P0`, `done`)
  - Define a registry for connector categories: databases, filesystems/blob stores, event streams, message queues, REST APIs, productivity tools, SaaS applications, geospatial systems, media sources, and generic connectors.
  - Store per-connector supported capabilities, worker compatibility, credential fields, network requirements, setup docs link, and feature flags.
  - Render capability tags on new-source cards and source overview pages.
  - Implemented in `apps/web/src/lib/api/data-connection.ts`, `apps/web/src/routes/data-connection/NewSourcePage.tsx`, and `apps/web/src/routes/data-connection/SourceDetailPage.tsx` with category metadata, registry normalization, category filtering, card tags, and overview/capabilities registry summaries.
  - Docs: [Source type overview](https://www.palantir.com/docs/foundry/data-integration/source-type-overview/), [Data Connection core concepts](https://www.palantir.com/docs/foundry/data-connection/core-concepts/), [Available connectors: other source types](https://www.palantir.com/docs/foundry/available-connectors/other-source-types/).

- [x] `SDC.3` Source CRUD and overview page (`P0`, `done`)
  - Create, get, list, update, archive/delete, and duplicate sources.
  - Track source name, description, connector type, project/folder, owner, worker, network policy, credential references, default output location, supported capabilities, health, usage, and audit metadata.
  - Provide source tabs for Overview, Configuration, Credentials, Networking, Explore, Syncs, Exports, Webhooks, Virtual Tables, Code imports, Permissions, and History.
  - Implemented in `apps/web/src/lib/api/data-connection.ts` and `apps/web/src/routes/data-connection/SourceDetailPage.tsx` with expanded source metadata, update/archive/duplicate API calls, editable source metadata, overview summary cards, and the complete source tab set.
  - Docs: [Set up a source](https://www.palantir.com/docs/foundry/data-connection/set-up-source), [Data Connection core concepts](https://www.palantir.com/docs/foundry/data-connection/core-concepts/).

- [x] `SDC.4` Worker selection and compatibility validation (`P0`, `done`)
  - Support OpenFoundry worker and agent worker choices.
  - Validate that selected worker is allowed for the connector type and capability being configured.
  - Explain which capabilities are unavailable for each worker/source combination.
  - Implemented in `apps/web/src/lib/api/data-connection.ts`, `apps/web/src/routes/data-connection/NewSourcePage.tsx`, and `apps/web/src/routes/data-connection/SourceDetailPage.tsx` with per-worker capability metadata, compatibility validation helpers, disabled incompatible worker choices, and allowed/unavailable capability explanations in source creation and source editing.
  - Docs: [Data Connection core concepts](https://www.palantir.com/docs/foundry/data-connection/core-concepts/), [Set up a source](https://www.palantir.com/docs/foundry/data-connection/set-up-source), [Initial setup overview](https://www.palantir.com/docs/foundry/data-connection/initial-setup-overview/).

- [x] `SDC.5` Credential storage and rotation metadata (`P0`, `done`)
  - Store encrypted credentials or references to external secrets.
  - Support username/password, API key, bearer token, OAuth/client secret, cloud identity reference, certificate/key, and connector-specific secret fields.
  - Track secret version, last rotated, created by, credential test status, source usage, and audit events without exposing secret values.
  - Implemented in `apps/web/src/lib/api/data-connection.ts` and `apps/web/src/routes/data-connection/SourceDetailPage.tsx` with credential storage modes, secret/reference metadata, rotation/test/usage/audit fields, rotate/test API helpers, and write-only credential forms.
  - Docs: [Data Connection core concepts](https://www.palantir.com/docs/foundry/data-connection/core-concepts/), [Set up a source](https://www.palantir.com/docs/foundry/data-connection/set-up-source).

- [x] `SDC.6` Network egress policy integration (`P0`, `done`)
  - Create and attach direct egress policies for Foundry-worker connections.
  - Create and attach agent proxy policies for agent-mediated private-network connections where supported.
  - Validate host, port, protocol, proxy mode, policy status, and allowed organizations before connection tests.
  - Implemented in `apps/web/src/lib/api/data-connection.ts`, `apps/web/src/lib/components/data-connection/CreateEgressPolicyModal.tsx`, `apps/web/src/routes/data-connection/EgressPoliciesPage.tsx`, and `apps/web/src/routes/data-connection/SourceDetailPage.tsx` with protocol/proxy/status/org metadata, shared egress validation helpers, direct/agent-proxy create-and-attach flows, and active-policy pre-test validation.
  - Docs: [Initial setup overview](https://www.palantir.com/docs/foundry/data-connection/initial-setup-overview/), [Configure egress](https://www.palantir.com/docs/foundry/administration/configure-egress/), [Agent proxy configuration reference](https://www.palantir.com/docs/foundry/data-connection/agent-proxy/).

- [x] `SDC.7` Source connection tests and exploration (`P0`, `done`)
  - Test connectivity, authentication, permissions, and simple metadata discovery from source setup and overview pages.
  - Browse external folders, files, databases, schemas, tables, topics, queues, streams, and sample rows where supported.
  - Store exploration sessions without persisting secret values or unauthorized sample data.
  - Implemented in `apps/web/src/lib/api/data-connection.ts`, `apps/web/src/lib/components/data-connection/TestConnectionPanel.tsx`, `apps/web/src/routes/data-connection/NewSourcePage.tsx`, and `apps/web/src/routes/data-connection/SourceDetailPage.tsx` with structured connection-test checks, exploration session/node models, source setup discovery details, source detail exploration browsing, redacted sample metadata, and no-secret/no-sample persistence indicators.
  - Docs: [Set up a source](https://www.palantir.com/docs/foundry/data-connection/set-up-source), [Data Connection core concepts](https://www.palantir.com/docs/foundry/data-connection/core-concepts/).

### Batch, file, and table sync basics

- [x] `SDC.8` Generic sync resource model (`P0`, `done`)
  - Create sync resources from a source and capability type.
  - Track output dataset/stream/media set, source path/table/topic, schema, write mode, transaction mode, schedule/build integration, last run, next run, health, and history.
  - Creating a batch sync should create or select an OpenFoundry dataset output.
  - Implemented in `apps/web/src/lib/api/data-connection.ts` and `apps/web/src/routes/data-connection/SourceDetailPage.tsx` with generic sync capability/output/write/transaction models, schema/run/health/history metadata, output dataset suggestion helpers, create-or-select dataset payloads, and a richer sync creation/listing UI.
  - Docs: [Set up a sync](https://www.palantir.com/docs/foundry/data-connection/set-up-sync), [Data Connection core concepts](https://www.palantir.com/docs/foundry/data-connection/core-concepts/).

- [x] `SDC.9` File-based sync modes (`P0`, `done`)
  - Support default snapshot mirror, incremental append, historical snapshot plus incremental recent files, exclude-already-synced filters, file count limits, include/exclude glob filters, and path metadata columns where available.
  - Store low-level settings and warn on contradictory options.
  - Emit dataset transactions consistent with the selected mode.
  - Implemented in `apps/web/src/lib/api/data-connection.ts`, `apps/web/src/lib/api/data-connection.test.ts`, and `apps/web/src/routes/data-connection/SourceDetailPage.tsx` with file sync modes, low-level filter metadata, contradictory-setting warnings, file-count validation, path metadata columns, and dataset transaction mapping for SNAPSHOT/APPEND/UPDATE planning.
  - Docs: [File-based syncs](https://www.palantir.com/docs/foundry/data-connection/file-based-syncs/), [Set up a sync](https://www.palantir.com/docs/foundry/data-connection/set-up-sync).

- [x] `SDC.10` Table batch syncs (`P0`, `done`)
  - Browse tables, select one or more tables, infer schema, configure output dataset location, and run syncs.
  - Support full snapshot and incremental modes where the connector can identify new or changed records.
  - Capture source table schema, destination schema, row counts, transaction IDs, and run history.
  - Implemented in `apps/web/src/lib/api/data-connection.ts`, `apps/web/src/lib/api/data-connection.test.ts`, and `apps/web/src/routes/data-connection/SourceDetailPage.tsx` with table selection models, schema/row-count/transaction metadata, full snapshot and incremental transaction mapping, incremental-column warnings, and source-detail controls for table batch sync creation.
  - Docs: [Set up a sync](https://www.palantir.com/docs/foundry/data-connection/set-up-sync), [Data Connection core concepts](https://www.palantir.com/docs/foundry/data-connection/core-concepts/).

- [x] `SDC.11` Sync run lifecycle and history (`P0`, `done`)
  - Track queued, running, succeeded, failed, cancelled, retrying, ignored, and partially successful states.
  - Persist start/end time, duration, worker/agent, build/job ID, source offsets or file checkpoints, output transaction, rows/files/bytes transferred, retries, and logs.
  - Link sync run history to Data Foundation build/job history where the sync is executed by the build system.
  - Implemented in `apps/web/src/lib/api/data-connection.ts`, `apps/web/src/lib/api/data-connection.test.ts`, and `apps/web/src/routes/data-connection/SourceDetailPage.tsx` with expanded run states, duration/build-link helpers, source progress, output transactions, retry/log metadata, and richer run-history rendering linked to build/job history.
  - Docs: [Set up a sync](https://www.palantir.com/docs/foundry/data-connection/set-up-sync), [Builds core concepts](https://www.palantir.com/docs/foundry/data-integration/builds/).

### Streams and streaming sync basics

- [x] `SDC.12` Stream resource model (`P0`, `done`)
  - Model streams as tabular resources with schema, permissions, branch, hot buffer, cold/archive dataset, consistency guarantee, checkpoints, and replay metadata.
  - Expose stream details, live view, archive/dataset view, schema, offsets, checkpoints, source syncs, consumers, and health.
  - Implemented in `apps/web/src/lib/api/data-connection.ts`, `apps/web/src/lib/api/data-connection.test.ts`, and `apps/web/src/routes/data-connection/SourceDetailPage.tsx` with stream resource models, storage/offset/checkpoint/replay/consumer/permission metadata, stream API helpers, replay/storage labels, and a source-detail Streams tab.
  - Docs: [Streams core concepts](https://www.palantir.com/docs/foundry/data-integration/streams/).

- [x] `SDC.13` Streaming sync setup (`P0`, `done`)
  - Create long-running syncs from supported streaming sources to OpenFoundry streams.
  - Configure source topic/queue/stream, consumer group or equivalent, schema, key fields, start offset, consistency guarantee, checkpoint interval, and output stream location.
  - Use start/stop controls rather than one-shot run controls.
  - Implemented in `apps/web/src/lib/api/data-connection.ts`, `apps/web/src/lib/api/data-connection.test.ts`, and `apps/web/src/routes/data-connection/SourceDetailPage.tsx` with streaming sync setup/status models, validation helpers, create/start/stop API methods, and source-detail controls for topic, consumer group, key fields, offset, consistency, checkpoint interval, and stream location.
  - Docs: [Set up a streaming sync](https://www.palantir.com/docs/foundry/data-connection/set-up-streaming-sync/), [Streams core concepts](https://www.palantir.com/docs/foundry/data-integration/streams/).

- [x] `SDC.14` Stream hot/cold storage and hybrid reads (`P0`, `done`)
  - Store recent records in a low-latency hot buffer.
  - Archive stream records to cold storage as a dataset on a fixed cadence or configurable policy.
  - Provide a hybrid read path that combines hot and cold storage for stream-aware applications.
  - Implemented in `apps/web/src/lib/api/data-connection.ts`, `apps/web/src/lib/api/data-connection.test.ts`, and `apps/web/src/routes/data-connection/SourceDetailPage.tsx` with archive policy metadata, hybrid read metadata/API response types, hybrid read API helper, hot/cold summary labels, and stream-tab archive/hybrid status rendering.
  - Docs: [Streams core concepts](https://www.palantir.com/docs/foundry/data-integration/streams/).

- [x] `SDC.15` Stream checkpoints and restart behavior (`P0`, `done`)
  - Persist checkpoint state, last processed source location, operator state metadata, size, duration, status, and timestamp.
  - Restart failed streaming jobs from the latest completed checkpoint when possible.
  - Show recent checkpoint status on stream job detail pages.
  - Implemented in `apps/web/src/lib/api/data-connection.ts`, `apps/web/src/lib/api/data-connection.test.ts`, and `apps/web/src/routes/data-connection/SourceDetailPage.tsx` with checkpoint status/source-location/operator-state metadata, restart plan helpers, latest-completed checkpoint selection, and stream-tab checkpoint/restart status rendering.
  - Docs: [Streams core concepts](https://www.palantir.com/docs/foundry/data-integration/streams/).

- [x] `SDC.16` Streaming consistency modes (`P0`, `done`)
  - Support at-least-once mode and document duplicate-tolerant consumer requirements.
  - Support exactly-once mode where the selected runtime and sink can guarantee it.
  - Block or downgrade exactly-once mode when a source/sink combination cannot provide the guarantee.
  - Implemented in `apps/web/src/lib/api/data-connection.ts`, `apps/web/src/lib/api/data-connection.test.ts`, and `apps/web/src/routes/data-connection/SourceDetailPage.tsx` with runtime/source/sink consistency evaluation, exactly-once downgrade warnings, duplicate-tolerant consumer guidance, and stream-tab effective consistency summaries.
  - Docs: [Streams core concepts](https://www.palantir.com/docs/foundry/data-integration/streams/).

- [x] `SDC.17` Push-based stream ingestion (`P0`, `done`)
  - Provide authenticated REST endpoints for pushing records into streams by stream/dataset ID and branch.
  - Validate schema, token, branch, rate limits, record count, and idempotency key where supported.
  - Recommend streaming syncs when a source connector exists and listeners when inbound systems cannot authenticate or conform to stream schemas.
  - Implemented in `apps/web/src/lib/api/data-connection.ts`, `apps/web/src/lib/api/data-connection.test.ts`, and `apps/web/src/routes/data-connection/SourceDetailPage.tsx` with push endpoint descriptors, authenticated record push request/response types, schema/token/branch/rate/idempotency validation, ingestion recommendations, and stream-tab push controls.
  - Docs: [Push data into a stream](https://www.palantir.com/docs/foundry/data-connection/push-based-ingestion/), [Streams core concepts](https://www.palantir.com/docs/foundry/data-integration/streams/).

### Webhook and REST source basics

- [x] `SDC.18` REST API source and webhook setup (`P0`, `done`)
  - Create REST API sources with base domains, auth configuration, additional secrets, network policy, worker selection, and source permissions.
  - Create webhooks associated with one source.
  - Configure method, relative path, query parameters, headers, body, authorization references, and timeout/retry behavior.
  - Implemented in `apps/web/src/lib/api/data-connection.ts`, `apps/web/src/lib/api/data-connection.test.ts`, `apps/web/src/routes/data-connection/NewSourcePage.tsx`, and `apps/web/src/routes/data-connection/SourceDetailPage.tsx` with REST API source setup/auth models, webhook request/retry models, validation helpers, REST source wizard fields, source-scoped webhook APIs, and a Webhooks tab creation/listing flow.
  - Docs: [Webhooks overview](https://www.palantir.com/docs/foundry/data-connection/webhooks-overview), [Set up a Webhook](https://www.palantir.com/docs/foundry/data-connection/webhooks-setup/), [Webhooks configuration reference](https://www.palantir.com/docs/foundry/data-connection/webhooks-reference).

- [x] `SDC.19` Webhook input and output parameters (`P0`, `done`)
  - Support Boolean, integer, long, double, string, date, timestamp, list, record, optional, and attachment parameter metadata where locally supported.
  - Map action/function parameters into webhook inputs and allow conditional skip when mapping returns no request.
  - Extract outputs using whole response, key path, array index, JSON extractor, HTTP status, and full response string where supported.
  - Implemented in `apps/web/src/lib/api/data-connection.ts`, `apps/web/src/lib/api/data-connection.test.ts`, and `apps/web/src/routes/data-connection/SourceDetailPage.tsx` with webhook input/output parameter metadata, typed mapping validation, conditional invocation skip support, output extractors, and Webhooks tab parameter editors.
  - Docs: [Webhooks configuration reference](https://www.palantir.com/docs/foundry/data-connection/webhooks-reference).

- [x] `SDC.20` Webhook invocation history (`P0`, `done`)
  - Record invocation time, caller, action/function context, source, webhook, input parameter summary, HTTP status, parsed outputs, error, retry attempts, and redacted request/response metadata.
  - Enforce secret redaction and payload retention limits.
  - Implemented in `apps/web/src/lib/api/data-connection.ts`, `apps/web/src/lib/api/data-connection.test.ts`, and `apps/web/src/routes/data-connection/SourceDetailPage.tsx` with invocation history records, source/webhook/caller/action/function metadata, parsed output summaries, redacted request/response metadata helpers, retention filtering, and Webhooks tab history loading.
  - Docs: [Webhooks configuration reference](https://www.palantir.com/docs/foundry/data-connection/webhooks-reference), [Webhooks overview](https://www.palantir.com/docs/foundry/data-connection/webhooks-overview).

## Milestone B: credible Foundry-style Data Connection and streaming parity

### Change data capture

- [x] `SDC.21` CDC sync setup (`P1`, `done`)
  - Create CDC syncs for supported relational connectors and changelog-shaped streaming middleware inputs.
  - Capture source table, primary key columns, ordering column, deletion column, output stream, schema, start position, and connector-derived metadata.
  - Validate that source tables and databases are configured to expose changelog data.
  - Implemented in `apps/web/src/lib/api/data-connection.ts`, `apps/web/src/lib/api/data-connection.test.ts`, `apps/web/src/routes/data-connection/SourceDetailPage.tsx`, `services/connector-management-service/internal/models/models.go`, `services/connector-management-service/internal/repo/repo.go`, `services/connector-management-service/internal/handlers/handlers.go`, and `services/connector-management-service/internal/repo/migrations/20260513000001_cdc_sync_setup.sql` with CDC setup models, supported relational/changelog connector detection, source changelog-readiness validation, schema/PK/order/delete/start-position capture, stream output persistence, Source Detail CDC creation controls, and backend guards that keep stream CDC syncs out of one-shot batch runs.
  - Docs: [Change data capture](https://www.palantir.com/docs/foundry/data-integration/change-data-capture).

- [x] `SDC.22` CDC live and archive views (`P1`, `done`)
  - Display live changelog entries and archive/current-state view for CDC streams.
  - Resolve archive view by ordering column and deletion marker according to configured resolution strategy.
  - Show primary key resolution strategy in stream schema details.
  - Implemented in `apps/web/src/lib/api/data-connection.ts`, `apps/web/src/lib/api/data-connection.test.ts`, and `apps/web/src/routes/data-connection/SourceDetailPage.tsx` with deterministic CDC archive resolution from live changelog rows, backend archive fallback, schema-level primary key/order/delete role labels, and side-by-side live/current-state stream views in Source Detail.
  - Docs: [Change data capture](https://www.palantir.com/docs/foundry/data-integration/change-data-capture), [Streams core concepts](https://www.palantir.com/docs/foundry/data-integration/streams/).

- [x] `SDC.23` CDC downstream integration (`P1`, `done`)
  - Make CDC metadata available to Pipeline Builder, Ontology indexing, stream processing, archive views, and Data Health checks.
  - Warn that custom or manually backfilled changelog streams must preserve ordering semantics before object indexing.
  - Implemented in `apps/web/src/lib/api/data-connection.ts`, `apps/web/src/lib/api/data-connection.test.ts`, and `apps/web/src/routes/data-connection/SourceDetailPage.tsx` with a downstream CDC metadata bundle for Pipeline Builder Key By configuration, Ontology indexing, stream processing, archive/current-state resolution, and Data Health recommended checks, plus ordering-preservation warnings for custom, streaming-middleware, or manually backfilled changelog streams before object indexing.
  - Docs: [Change data capture](https://www.palantir.com/docs/foundry/data-integration/change-data-capture), [Create incremental syncs](https://www.palantir.com/docs/foundry/building-pipelines/create-incremental-syncs/).

### Virtual tables and virtualized data

- [x] `SDC.24` Virtual table registration (`P1`, `done`)
  - Register individual virtual tables from supported source systems.
  - Support bulk registration for tabular source types and automatic registration where product policy enables it.
  - Store external database/schema/table references, display name, save location, source, schema, owner, and permissions.
  - Implemented in `services/connector-management-service/internal/models/models.go`, `services/connector-management-service/internal/repo/repo.go`, `services/connector-management-service/internal/handlers/handlers.go`, `services/connector-management-service/internal/server/server.go`, `apps/web/src/lib/api/virtual-tables.ts`, `apps/web/src/lib/api/virtual-tables.test.ts`, `apps/web/src/lib/components/data-connection/CreateVirtualTableModal.tsx`, `apps/web/src/lib/components/data-connection/BulkRegisterDialog.tsx`, `apps/web/src/routes/virtual-tables/VirtualTablesPage.tsx`, and `apps/web/src/routes/virtual-tables/VirtualTableDetailPage.tsx` with individual registration metadata capture, tabular bulk registration, policy-gated auto-registration state, catalog discovery, owner/permission/schema persistence, and list/detail views for external references and save locations.
  - Docs: [Virtual tables](https://www.palantir.com/docs/foundry/data-integration/virtual-tables/).

- [x] `SDC.25` Virtual table query and pushdown model (`P1`, `done`)
  - Query virtual tables without first copying data into OpenFoundry datasets.
  - Track whether computation happens in OpenFoundry, source system, or hybrid pushdown.
  - Expose limitations for interactive performance, compute usage, unsupported features, and unsupported worker/network modes.
  - Implemented in `services/connector-management-service/internal/models/models.go`, `services/connector-management-service/internal/handlers/handlers.go`, `services/connector-management-service/internal/handlers/handlers_test.go`, `services/connector-management-service/internal/server/server.go`, `apps/web/src/lib/api/virtual-tables.ts`, `apps/web/src/lib/api/virtual-tables.test.ts`, and `apps/web/src/routes/virtual-tables/VirtualTableDetailPage.tsx` with a direct virtual-table query endpoint, adapter-backed or metadata-backed previews that never create copied datasets, source/OpenFoundry/hybrid pushdown plans, compute-location metadata, and limitations for interactive latency, source/OpenFoundry compute, partial pushdown, and unsupported worker/egress modes.
  - Docs: [Virtual tables](https://www.palantir.com/docs/foundry/data-integration/virtual-tables/).

- [x] `SDC.26` Virtual table update detection and lineage (`P1`, `done`)
  - Detect source-side table updates where the connector supports versioning or update detection.
  - Use update detection to skip unnecessary downstream builds.
  - Show virtual table lineage from source through downstream datasets, object outputs, and pipelines.
  - Implemented in `services/connector-management-service/internal/models/models.go`, `services/connector-management-service/internal/repo/repo.go`, `services/connector-management-service/internal/handlers/handlers.go`, `services/connector-management-service/internal/handlers/handlers_test.go`, `services/connector-management-service/internal/server/server.go`, `apps/web/src/lib/api/virtual-tables.ts`, `apps/web/src/lib/api/virtual-tables.test.ts`, `apps/web/src/lib/components/data-connection/VirtualTableDetailsPanel.tsx`, and `apps/web/src/routes/virtual-tables/VirtualTableDetailPage.tsx` with version-aware update polling, persisted poll history, unchanged-version downstream build skips, synthetic and import-backed lineage nodes/edges for source, virtual table, pipelines, datasets, and object outputs, and UI surfaces for poll decisions and lineage build impact.
  - Docs: [Virtual tables](https://www.palantir.com/docs/foundry/data-integration/virtual-tables/).

- [x] `SDC.27` Virtual tables as pipeline inputs and outputs (`P1`, `done`)
  - Allow virtual tables as Pipeline Builder/code transform inputs where supported.
  - Allow transforms to create virtual tables as outputs when OpenFoundry handles orchestration but storage remains external.
  - Block unsupported workflows such as incompatible external-system decorators or unsupported host applications.
  - Implemented in `services/pipeline-build-service/internal/handler/schema_validation.go`, `services/pipeline-build-service/internal/handler/virtual_table_workflow_validation.go`, `services/pipeline-build-service/internal/handler/execution.go`, `services/pipeline-build-service/internal/domain/executor/executor.go`, `apps/web/src/lib/api/virtual-tables.ts`, `apps/web/src/lib/components/pipeline/AddFoundryDataDialog.tsx`, `apps/web/src/lib/components/pipeline/PipelineCanvas.tsx`, `apps/web/src/lib/components/pipeline/OutputDrawer.tsx`, and `apps/web/src/routes/pipelines/PipelineEditPage.tsx` with supported virtual-table input selection, external-storage virtual-table outputs, output transaction metadata, Pipeline Builder/code repository capability gates, and explicit validation blocks for streaming/Faster/unsupported hosts and legacy `use_external_systems` transforms.
  - Docs: [Virtual tables](https://www.palantir.com/docs/foundry/data-integration/virtual-tables/), [External transforms](https://www.palantir.com/docs/foundry/data-connection/external-transforms).

### Exports

- [x] `SDC.28` Export resource model (`P1`, `done`)
  - Create file, table, and streaming export resources from sources that support export capabilities.
  - Track input dataset or stream, destination path/table/topic, export type, export mode, schedule/start-stop behavior, export controls, history, and health.
  - Implemented in `services/connector-management-service/internal/models/models.go`, `services/connector-management-service/internal/repo/repo.go`, `services/connector-management-service/internal/repo/migrations/20260513000002_data_exports.sql`, `services/connector-management-service/internal/handlers/handlers.go`, `services/connector-management-service/internal/server/server.go`, `apps/web/src/lib/api/data-connection.ts`, and `apps/web/src/routes/data-connection/SourceDetailPage.tsx` with connector-gated file/table/streaming export resources, persisted input/destination/mode/schedule/start-stop fields, export controls, health/history state, and UI actions for run/start/stop.
  - Docs: [Exports overview](https://www.palantir.com/docs/foundry/data-connection/export-overview/), [Data Connection core concepts](https://www.palantir.com/docs/foundry/data-connection/core-concepts/).

- [x] `SDC.29` File exports (`P1`, `done`)
  - Export raw files from an input dataset to external filesystems or blob stores.
  - By default, export only files modified since the last successfully exported transaction.
  - Support overwrite behavior configuration, destination subfolder guidance, full re-export workaround, and export history.
  - Implemented in `services/connector-management-service/internal/models/models.go`, `services/connector-management-service/internal/repo/repo.go`, `services/connector-management-service/internal/repo/migrations/20260513000003_file_export_settings.sql`, `services/connector-management-service/internal/handlers/handlers_test.go`, `apps/web/src/lib/api/data-connection.ts`, `apps/web/src/lib/api/data-connection.test.ts`, and `apps/web/src/routes/data-connection/SourceDetailPage.tsx` with typed file export settings, modified-since-last-success run planning, overwrite behavior, destination subfolder guidance, one-off full re-export support, source file manifests, high-watermark updates, and file-count/byte-count export history.
  - Docs: [Exports overview](https://www.palantir.com/docs/foundry/data-connection/export-overview/).

- [x] `SDC.30` Table exports (`P1`, `done`)
  - Export schema-bearing dataset rows into external database tables.
  - Support efficient mirror mode and full dataset without truncation mode.
  - Validate exact column names, types, Parquet-backed input, destination table existence, truncate permission, and unsupported nested types.
  - Implemented in `services/connector-management-service/internal/models/models.go`, `services/connector-management-service/internal/repo/repo.go`, `services/connector-management-service/internal/repo/migrations/20260513000004_table_export_settings.sql`, `services/connector-management-service/internal/handlers/handlers.go`, `services/connector-management-service/internal/handlers/handlers_test.go`, `apps/web/src/lib/api/data-connection.ts`, `apps/web/src/lib/api/data-connection.test.ts`, and `apps/web/src/routes/data-connection/SourceDetailPage.tsx` with persisted table export settings, strict schema/type/Parquet/destination/truncate validations, efficient mirror and full-snapshot-without-truncation run planning, row/truncate history, and UI controls for table schemas and execution policy.
  - Docs: [Exports overview](https://www.palantir.com/docs/foundry/data-connection/export-overview/).

- [x] `SDC.31` Streaming exports (`P1`, `done`)
  - Export records continuously from an OpenFoundry stream to supported external queues/topics while the export job is running.
  - Support start/stop controls, restart from previous export offset, schedule-triggered restart, and replay behavior selection.
  - Provide duplicate/drop warning when exporting replayed records or skipping replayed records.
  - Implemented in `services/connector-management-service/internal/models/models.go`, `services/connector-management-service/internal/repo/repo.go`, `services/connector-management-service/internal/repo/migrations/20260513000005_streaming_export_settings.sql`, `services/connector-management-service/internal/handlers/handlers.go`, `services/connector-management-service/internal/handlers/handlers_test.go`, `apps/web/src/lib/api/data-connection.ts`, `apps/web/src/lib/api/data-connection.test.ts`, and `apps/web/src/routes/data-connection/SourceDetailPage.tsx` with persisted streaming export settings, replay behavior warnings, previous-offset restart planning, schedule restart metadata, start/stop state transitions, offset/record history, and UI controls for replay and offset policy.
  - Docs: [Exports overview](https://www.palantir.com/docs/foundry/data-connection/export-overview/), [Streams core concepts](https://www.palantir.com/docs/foundry/data-integration/streams/).

- [x] `SDC.32` Export scheduling and history (`P1`, `done`)
  - Schedule file and table exports through the same schedule/build system used for datasets.
  - Show schedules that trigger an export from the export overview page.
  - Show export job history, build report links, row/file counts, offsets, errors, and retry attempts.
  - Implemented in `services/connector-management-service/internal/models/models.go`, `services/connector-management-service/internal/repo/repo.go`, `services/connector-management-service/internal/handlers/handlers_test.go`, `apps/web/src/lib/api/data-connection.ts`, `apps/web/src/lib/api/data-connection.test.ts`, and `apps/web/src/routes/data-connection/SourceDetailPage.tsx` with schedule metadata for file/table exports, build report IDs/links, retry/error fields, schedule-triggered job history, and export overview rendering for schedule plus row/file/record/offset counters.
  - Docs: [Exports overview](https://www.palantir.com/docs/foundry/data-connection/export-overview/), [Schedules core concepts](https://www.palantir.com/docs/foundry/data-integration/schedules/).

### Sources from code and extensibility

- [x] `SDC.33` Source imports in Python transforms (`P1`, `done`)
  - Allow approved sources to be imported into Python transforms through generated bindings.
  - Render imported sources as links and friendly names in code repositories.
  - Pick up credential, egress, and exportable-marking changes from source configuration at build start without requiring code changes.
  - Implemented in `services/connector-management-service/internal/models/models.go`, `services/connector-management-service/internal/repo/repo.go`, `services/connector-management-service/internal/repo/migrations/20260513000006_source_code_imports.sql`, `services/connector-management-service/internal/handlers/handlers.go`, `services/connector-management-service/internal/server/server.go`, `apps/web/src/lib/api/data-connection.ts`, `apps/web/src/lib/api/data-connection.test.ts`, and `apps/web/src/routes/data-connection/SourceDetailPage.tsx` with source-level code-import approval, generated `@external_systems` bindings, repository import links/friendly names, exportable marking policy, and build-start resolution of live source config, credentials, and egress bindings without changing transform code.
  - Docs: [External transforms](https://www.palantir.com/docs/foundry/data-connection/external-transforms), [Sources in Python environments](https://www.palantir.com/docs/foundry/data-connection/sources-in-python/).

- [x] `SDC.34` External transform patterns (`P1`, `done`)
  - Support code-based alternatives for batch sync, file export, table batch sync, table export, media sync handoffs, virtual table registration, and virtual media registration.
  - Provide examples for REST APIs, databases, buffered Parquet writes, CSV exports, lightweight transforms, and private network access through agent proxy policies.
  - Implemented in `services/connector-management-service/internal/models/models.go`, `services/connector-management-service/internal/repo/repo.go`, `services/connector-management-service/internal/handlers/handlers_test.go`, `apps/web/src/lib/api/data-connection.ts`, `apps/web/src/lib/api/data-connection.test.ts`, and `apps/web/src/routes/data-connection/SourceDetailPage.tsx` with generated external-transform pattern cards/snippets on source code imports, coverage helpers for code-based alternatives, and examples for REST API syncs, custom database reads/writes, buffered Parquet commits, CSV exports, media handoff manifests, virtual table/media registration payloads, lightweight lookups, and agent-proxy private-network access.
  - Docs: [External transforms](https://www.palantir.com/docs/foundry/data-connection/external-transforms), [Data Connection core concepts](https://www.palantir.com/docs/foundry/data-connection/core-concepts/).

- [x] `SDC.35` Compute module alternatives (`P1`, `blocked`)
  - Support compute-module-style alternatives for streaming syncs, streaming exports, CDC syncs, and webhooks when OpenFoundry has long-running arbitrary-language compute modules.
  - Mark blocked until compute module runtime, deployment, and source-import contracts exist.
  - Implemented in `services/connector-management-service/internal/models/models.go`, `services/connector-management-service/internal/repo/repo.go`, `services/connector-management-service/internal/handlers/handlers_test.go`, `services/connector-management-service/internal/server/server_test.go`, `apps/web/src/lib/api/data-connection.ts`, `apps/web/src/lib/api/data-connection.test.ts`, and `apps/web/src/routes/data-connection/SourceDetailPage.tsx` with blocked compute-module alternative descriptors for streaming syncs, streaming exports, CDC syncs, and webhooks, including runtime/deployment/source-import blockers, arbitrary-language readiness checks, source-bound code sketches, and Source Detail rendering while keeping the product status blocked until the runtime contracts exist.
  - Docs: [Data Connection core concepts](https://www.palantir.com/docs/foundry/data-connection/core-concepts/).

- [x] `SDC.36` Source export controls for code imports (`P1`, `done`)
  - Require source owners to explicitly enable whether Foundry inputs may be used in jobs with access to the external system.
  - Store exportable markings/organizations or OpenFoundry-native equivalents on the source.
  - Block builds that combine Foundry inputs with an external source unless the source export policy allows it.
  - Implemented in `services/connector-management-service/internal/models/models.go`, `services/connector-management-service/internal/repo/repo.go`, `services/connector-management-service/internal/handlers/handlers_test.go`, `services/connector-management-service/internal/repo/migrations/20260513000006_source_code_imports.sql`, `apps/web/src/lib/api/data-connection.ts`, `apps/web/src/lib/api/data-connection.test.ts`, and `apps/web/src/routes/data-connection/SourceDetailPage.tsx` with explicit `allow_foundry_inputs` source export controls, exportable marking/organization storage, build-start Foundry input policy decisions, blocking reasons for disabled or mismatched policies, and Source Detail controls/status rendering.
  - Docs: [External transforms](https://www.palantir.com/docs/foundry/data-connection/external-transforms).

### Agents, permissions, and health

- [x] `SDC.37` Agent registry and heartbeat (`P1`, `done`)
  - Register agents with ID, version, environment, host, status, last heartbeat, connected sources, supported connector capabilities, and assigned proxy policies.
  - Show agent health and agent-related connection failures from source and admin pages.
  - Implemented in `services/connector-management-service/internal/models/models.go`, `services/connector-management-service/internal/repo/repo.go`, `services/connector-management-service/internal/repo/migrations/20260513000007_agent_registry_heartbeat.sql`, `services/connector-management-service/internal/handlers/handlers.go`, `services/connector-management-service/internal/handlers/handlers_test.go`, `services/connector-management-service/internal/models/testdata/wire_models.json`, `apps/web/src/lib/api/data-connection.ts`, `apps/web/src/lib/api/data-connection.test.ts`, `apps/web/src/routes/data-connection/AgentsPage.tsx`, `apps/web/src/routes/data-connection/DataConnectionPage.tsx`, and `apps/web/src/routes/data-connection/SourceDetailPage.tsx` with agent version/environment/host registration, heartbeat-enriched connected sources, connector capability summaries, assigned proxy policies, computed health/stale/failure status, and source/admin rendering for agent-related connection failures.
  - Docs: [Initial setup overview](https://www.palantir.com/docs/foundry/data-connection/initial-setup-overview/), [Agent proxy configuration reference](https://www.palantir.com/docs/foundry/data-connection/agent-proxy/).

- [x] `SDC.38` Source permissions and governance (`P1`, `done`)
  - Implement roles for source view, source edit, source use, source ownership, webhook execution, sync creation, export creation, and code import.
  - Enforce that source visibility, credential visibility, external sample visibility, and output dataset permissions are distinct.
  - Audit permission changes and source use by downstream jobs.
  - Implemented in `services/connector-management-service/internal/models/models.go`, `services/connector-management-service/internal/repo/repo.go`, `services/connector-management-service/internal/repo/migrations/20260513000008_source_permissions_governance.sql`, `services/connector-management-service/internal/handlers/handlers.go`, `services/connector-management-service/internal/handlers/registrations.go`, `services/connector-management-service/internal/handlers/handlers_test.go`, `services/connector-management-service/internal/server/server.go`, `services/connector-management-service/internal/server/server_test.go`, `apps/web/src/lib/api/data-connection.ts`, `apps/web/src/lib/api/data-connection.test.ts`, and `apps/web/src/routes/data-connection/SourceDetailPage.tsx` with source role grants, role implication helpers, separate credential/sample/output-dataset visibility policy, permissions/audit endpoints, role enforcement across credentials, policies, syncs, exports, webhooks, registration discovery, source testing, and code imports, plus Source Detail governance and audit rendering.
  - Docs: [Set up a source](https://www.palantir.com/docs/foundry/data-connection/set-up-source), [Data Connection core concepts](https://www.palantir.com/docs/foundry/data-connection/core-concepts/).

- [x] `SDC.39` Data Connection health checks (`P1`, `done`)
  - Monitor sources, agents, credentials, network policies, syncs, streams, exports, webhooks, CDC metadata, virtual tables, and schedules.
  - Surface recent failures, stale syncs, stream lag, checkpoint failures, agent offline, credential expiration, schema drift, destination schema mismatch, and export policy violations.
  - Implemented in `services/connector-management-service/internal/models/models.go`, `services/connector-management-service/internal/repo/repo.go`, `services/connector-management-service/internal/repo/migrations/20260513000009_data_connection_health_checks.sql`, `services/connector-management-service/internal/handlers/handlers.go`, `services/connector-management-service/internal/handlers/handlers_test.go`, `services/connector-management-service/internal/server/server.go`, `services/connector-management-service/internal/server/server_test.go`, `apps/web/src/lib/api/data-connection.ts`, `apps/web/src/lib/api/data-connection.test.ts`, and `apps/web/src/routes/data-connection/SourceDetailPage.tsx` with a source health aggregation endpoint, typed health states/surfaces/checks, agent heartbeat/failure checks, credential expiration/validation checks, network policy presence checks, sync freshness and stream checkpoint checks, CDC metadata validation, export health/schema/replay/schedule checks, webhook failure checks, virtual table update-detection checks, and a Source Detail Health tab that folds in client-visible stream lag/checkpoint status.
  - Docs: [Data Connection overview](https://www.palantir.com/docs/foundry/data-connection/overview), [Data Health](https://www.palantir.com/docs/foundry/observability/data-health/).

- [x] `SDC.40` Automatic retries and failure recovery (`P1`, `done`)
  - Implement retry/backoff policies for transient source, network, credential, and destination failures.
  - Preserve enough checkpoint/source state to avoid full reruns when possible.
  - Escalate persistent failures to Data Health and schedule/build history.
  - Implemented in `services/connector-management-service/internal/models/models.go`, `services/connector-management-service/internal/models/retry_recovery_test.go`, `services/connector-management-service/internal/repo/repo.go`, `services/connector-management-service/internal/repo/migrations/20260513000010_retry_recovery_policies.sql`, `services/connector-management-service/internal/handlers/handlers.go`, `services/connector-management-service/internal/handlers/handlers_test.go`, `services/connector-management-service/internal/server/server.go`, `services/connector-management-service/internal/server/server_test.go`, `apps/web/src/lib/api/data-connection.ts`, `apps/web/src/lib/api/data-connection.test.ts`, and `apps/web/src/routes/data-connection/SourceDetailPage.tsx` with per-source retry policies for source/network/credential/destination categories, a deterministic failure classifier, exponential backoff with category-specific allowlists and non-retryable patterns, a retry decision helper that schedules attempts, preserves checkpoints, and escalates after the configured threshold, a `RetryRecoverySummary` aggregate that feeds the SDC.39 Data Connection health summary with `retry_escalated`, `retry_exhausted`, and `retry_backoff_in_progress` checks, persistent retry policy + sync_run_failure storage, REST endpoints for retry policy read/update and the recovery aggregate, and a Source Detail Retries tab that edits backoff per category and renders recent decisions, preserved checkpoints, and next retry windows.
  - Docs: [Data Connection overview](https://www.palantir.com/docs/foundry/data-connection/overview), [Streams core concepts](https://www.palantir.com/docs/foundry/data-integration/streams/).

## Milestone C: advanced connector coverage, scale, and governance

### Advanced ingestion surfaces

- [x] `SDC.41` Media sync handoff (`P2`, `done`)
  - Set up media set syncs from supported file/media sources.
  - Delegate media schema, conversion, transformations, transactional policy, and media reference behavior to the Media Sets checklist.
  - Track source, selected paths, output media set, sync history, errors, and usage.
  - Implemented in `services/connector-management-service/internal/models/models.go`, `services/connector-management-service/internal/models/media_sync_handoff_test.go`, `services/connector-management-service/internal/repo/repo.go`, `services/connector-management-service/internal/repo/migrations/20260514000001_media_set_sync_runs.sql`, `services/connector-management-service/internal/handlers/handlers.go`, `services/connector-management-service/internal/handlers/handlers_test.go`, `services/connector-management-service/internal/server/server.go`, `services/connector-management-service/internal/server/server_test.go`, `apps/web/src/lib/api/data-connection.ts`, `apps/web/src/lib/api/data-connection.test.ts`, and `apps/web/src/routes/data-connection/SourceDetailPage.tsx` with a `ConnectorSupportsMediaSync` registry gate (`s3`/`onelake`/`abfs`) enforced on `CreateMediaSetSync`, a `media_set_sync_runs` table that records each handoff with selected paths, accepted/skipped/mismatched/dispatched counts, bytes accepted, schema mismatches, and runtime error, `RunMediaSetSync` persisting every execution before mapping runtime errors to HTTP status, a `MediaSetSyncUsageSummary` rollup folded into the list endpoint, a `ListMediaSetSyncRuns` history endpoint, an explicit `MediaSetSyncHandoffDelegation` describing what is owned by the Media Sets checklist (schema, conversion, transformations, transaction policy, media reference), and a Source Detail Media syncs tab that surfaces per-sync usage, last status, and inline run history with paths/bytes/errors.
  - Docs: [Set up a media set sync](https://www.palantir.com/docs/foundry/data-connection/media-set-sync), [Data Connection core concepts](https://www.palantir.com/docs/foundry/data-connection/core-concepts/).

- [x] `SDC.42` Virtual media handoff (`P2`, `blocked`)
  - Register external media files as virtual media items without copying media into OpenFoundry storage.
  - Block until Media Sets virtual media semantics and object storage authorization are defined locally.
  - Implemented in `services/connector-management-service/internal/models/models.go`, `services/connector-management-service/internal/models/virtual_media_handoff_test.go`, `services/connector-management-service/internal/handlers/handlers.go`, `services/connector-management-service/internal/handlers/handlers_test.go`, `services/connector-management-service/internal/server/server.go`, `apps/web/src/lib/api/data-connection.ts`, `apps/web/src/lib/api/data-connection.test.ts`, and `apps/web/src/routes/data-connection/SourceDetailPage.tsx` with a typed `VirtualMediaHandoffDescriptor` exposed at `GET /api/v1/data-connection/sources/{id}/virtual-media-handoff`. The descriptor advertises three blocked registration modes per supported connector (s3/onelake/abfs) — `VIRTUAL_MEDIA_SET_SYNC` dispatch, external transform handoff, and REST API registration — with explicit blockers (`media_sets_virtual_item_semantics`, `object_storage_authorization`, `external_credential_routing`, `virtual_item_update_detection`, plus per-mode contracts), readiness checks, required contracts, the media-sets-service registration request shape, the object storage authorization contract, and a Cedar-style authorization contract. Sources whose connector cannot expose physical paths return `status: not_supported` instead of a blocked descriptor; otherwise the descriptor explicitly cites MS.18–MS.20 as the unblocking work. The Source Detail Media syncs tab renders a `VirtualMediaHandoffPanel` showing the blocked reason, blocker chips, coverage modes, and per-mode registration sketches so operators see what handoff would look like once unblocked. The product status remains `blocked` until the Media Sets checklist defines virtual media item semantics and the platform agrees on an object storage authorization primitive.
  - Docs: [Data Connection core concepts](https://www.palantir.com/docs/foundry/data-connection/core-concepts/), [External transforms](https://www.palantir.com/docs/foundry/data-connection/external-transforms).

- [x] `SDC.43` Listener-style inbound ingestion (`P2`, `blocked`)
  - Support inbound webhook/listener flows for external systems that cannot authenticate with stream push APIs or conform to stream schemas.
  - Provide schema mapping, auth strategy, replay/idempotency controls, and dead-letter handling.
  - Mark blocked until public listener documentation or local product policy is sufficient to define exact semantics.
  - Implemented in `services/connector-management-service/internal/models/models.go`, `services/connector-management-service/internal/models/listener_inbound_test.go`, `services/connector-management-service/internal/handlers/handlers.go`, `services/connector-management-service/internal/handlers/handlers_test.go`, `services/connector-management-service/internal/server/server.go`, `apps/web/src/lib/api/data-connection.ts`, `apps/web/src/lib/api/data-connection.test.ts`, and `apps/web/src/routes/data-connection/SourceDetailPage.tsx` with a typed `ListenerInboundDescriptor` exposed at `GET /api/v1/data-connection/sources/{id}/listener-descriptor`. The descriptor enumerates the four product-policy facets — schema mapping, auth strategy, replay & idempotency, dead-letter handling — each with its own `status` (`available`/`partial`/`blocked`), what's wired today, blockers, readiness checks, required contracts, and a configuration sketch. Auth strategy and replay/idempotency are marked `partial` because HMAC/shared-secret webhook listeners and idempotency-key extraction already work; schema mapping and dead-letter handling are `blocked`. The descriptor also surfaces the existing inbound listener routes, the supported and blocked auth modes, the idempotency-key header allowlist, the default `1MB` payload cap, and the SDC.17 ingestion recommendation pointing operators to listeners when push isn't viable. The aggregate status remains `blocked` overall (per-facet aggregation collapses to the worst status), with an explicit `BlockedReason` citing public docs and product policy as the unblocking work. A new `ListenerInboundPanel` is rendered in the Source Detail Streams tab so users see, in context, which inbound flow works today and which contracts are still missing.
  - Docs: [Push data into a stream](https://www.palantir.com/docs/foundry/data-connection/push-based-ingestion/).

- [x] `SDC.44` Connector-specific capability packs (`P2`, `done`)
  - Implement capability packs for high-value connector families such as PostgreSQL, SQL Server, Oracle, Db2, Kafka, Kinesis, SQS, Pub/Sub, S3-compatible object stores, SFTP/FTPS, Foundry-to-Foundry, Snowflake, BigQuery, Databricks, and generic REST.
  - Each pack should declare supported sync/export/virtual/CDC/webhook/exploration capabilities and source-specific validation.
  - Implemented in `services/connector-management-service/internal/models/connector_capability_packs.go`, `services/connector-management-service/internal/models/connector_capability_packs_test.go`, `services/connector-management-service/internal/handlers/handlers.go`, `services/connector-management-service/internal/handlers/handlers_test.go`, `services/connector-management-service/internal/server/server.go`, `apps/web/src/lib/api/data-connection.ts`, `apps/web/src/lib/api/data-connection.test.ts`, and `apps/web/src/routes/data-connection/SourceDetailPage.tsx` with typed `ConnectorCapabilityPack` manifests for 18 connectors covering all required families (PostgreSQL, SQL Server, Oracle, Db2; Snowflake, BigQuery, Databricks; S3, OneLake, ABFS, GCS; SFTP/FTPS; Kafka, Kinesis, Amazon SQS, Google Pub/Sub; generic REST; Foundry-to-Foundry). Each pack declares typed `ConnectorCapabilityFlags` for batch/file/table/streaming/CDC/media sync, file/table/streaming export, virtual table, webhook, and exploration; an explicit `cdc_input_kind` matching SDC.21 (`relational_connector` vs. `streaming_middleware_changelog`); per-worker overrides (agent workers drop relational `table_export`); structured `ConnectorValidationRule` entries with severity (`required`/`recommended`/`informational`) and connector-specific guidance (PostgreSQL logical decoding, Oracle LogMiner, Kafka changelog shape, S3 media MIME enforcement, REST webhook secret, Foundry-to-Foundry marking policy, etc.); plus notes and docs URLs. Two read-only endpoints expose the manifests: `GET /api/v1/data-connection/capability-packs` and `GET /api/v1/data-connection/capability-packs/{connector_type}`. The Source Detail Capabilities tab renders a `ConnectorCapabilityPackPanel` that shows declared capability chips (dimmed for capabilities the current worker drops), the connector family, CDC input kind, notes, and per-capability validation rules. Helpers `connectorCapabilityPackChips`, `connectorCapabilityPackEffectiveFlags`, `connectorCapabilityPackValidationRulesFor`, and `connectorCapabilityFamilyLabel` are exported and unit-tested.
  - Docs: [Source type overview](https://www.palantir.com/docs/foundry/data-integration/source-type-overview/), [Change data capture](https://www.palantir.com/docs/foundry/data-integration/change-data-capture), [Exports overview](https://www.palantir.com/docs/foundry/data-connection/export-overview/).

### Streaming scale and operations

- [x] `SDC.45` Stream lag and throughput metrics (`P2`, `done`)
  - Track ingestion rate, consumption rate, hot buffer size, archive lag, checkpoint duration, checkpoint size, processing lag, retries, and dropped/duplicate warnings.
  - Show metrics per stream, streaming sync, streaming export, topic/partition, and consumer.
  - Implemented in `services/connector-management-service/internal/models/stream_metrics.go`, `services/connector-management-service/internal/models/stream_metrics_test.go`, `services/connector-management-service/internal/handlers/handlers.go`, `services/connector-management-service/internal/handlers/handlers_test.go`, `services/connector-management-service/internal/server/server.go`, `apps/web/src/lib/api/data-connection.ts`, `apps/web/src/lib/api/data-connection.test.ts`, and `apps/web/src/routes/data-connection/SourceDetailPage.tsx` with a typed `StreamMetricsSnapshot` exposed via `POST /api/v1/data-connection/streams/metrics:compute`. The snapshot carries all nine SDC.45 metric families — ingestion rate, consumption rate, stream/hot buffer/archive/processing lag, checkpoint duration (avg/max/last), checkpoint size (avg/last), retries, dropped records, duplicate warnings, recent failures — and the five SDC.45 dimensions — per stream (top-level), per streaming sync, per streaming export, per topic/partition, and per consumer. The aggregator is a pure builder that sorts consumers and partitions by lag descending, derives per-window throughput in records/sec and bytes/sec, classifies failed checkpoints out of the duration average while preserving them in `failure_count`, and emits structured warnings for dropped records, duplicate detections, stalled consumption (lag exceeds the recent consumption rate), and per-export drop/duplicate risks. The frontend mirrors the shape with `formatStreamRate`, `streamMetricsHasWarning`, `streamMetricsWindowSeconds/Label`, and `streamMetricsInputFromResource`, and the Source Detail Streams tab renders a `StreamMetricsPanel` per stream card with a window selector (`1m`/`5m`/`1h`/`1d`), six metric cells, and collapsible breakdowns for consumers, topics/partitions, and syncs+exports.
  - Docs: [Streams core concepts](https://www.palantir.com/docs/foundry/data-integration/streams/), [Data Health](https://www.palantir.com/docs/foundry/observability/data-health/).

- [x] `SDC.46` Stream replay controls (`P2`, `done`)
  - Support safe replay after breaking processing logic changes.
  - Explain downstream implications for streaming exports, CDC archive views, object indexing, and duplicate-tolerant consumers.
  - Require explicit confirmation before replaying streams with active exports.
  - Implemented in `services/connector-management-service/internal/models/stream_replay.go`, `services/connector-management-service/internal/models/stream_replay_test.go`, `services/connector-management-service/internal/handlers/handlers.go`, `services/connector-management-service/internal/handlers/handlers_test.go`, `services/connector-management-service/internal/server/server.go`, `apps/web/src/lib/api/data-connection.ts`, `apps/web/src/lib/api/data-connection.test.ts`, and `apps/web/src/routes/data-connection/SourceDetailPage.tsx` with a typed `StreamReplayPlan` exposed via `POST /api/v1/data-connection/streams/replay-plan:compute`. The pure planner takes the replay window (from/to offsets, earliest/latest bounds, reason, requested_by, operator acknowledgements) plus a downstream inventory (active streaming exports, CDC archive views, object indexing pipelines, duplicate-tolerant consumers) and emits per-impact `severity` (`block`/`warn`/`info`), `implication`, `mitigation`, and `warning_id`, aggregates `preconditions_satisfied`/`preconditions_blocking`, derives the acknowledgement set (`acknowledgements_required` / `_satisfied` / `_missing`), and returns an aggregate `status` of `ready`/`requires_confirmation`/`blocked`. The active-export rule from the docs is enforced: any streaming export with `status: running` or active consumers raises a `block`-severity impact and a corresponding `ack_streaming_export_<id>` warning that must be explicitly confirmed before the plan flips to `ready`. CDC archive views without an ordering column are also blocking. The Source Detail Streams tab renders a `StreamReplayPlanPanel` per stream card with reason/from/to inputs, an "Evaluate replay plan" button, a status banner, impact cards sorted by severity (`block` first, `info` last), and per-impact acknowledgement checkboxes that re-feed the planner. Helpers `sortStreamReplayImpactsBySeverity`, `streamReplayDownstreamKindLabel`, `streamReplayImpactSeverityLabel`, and `streamReplayPlanRequiresAcknowledgement` are exported and unit-tested.
  - Docs: [Streams core concepts](https://www.palantir.com/docs/foundry/data-integration/streams/), [Exports overview](https://www.palantir.com/docs/foundry/data-connection/export-overview/), [Change data capture](https://www.palantir.com/docs/foundry/data-integration/change-data-capture).

- [x] `SDC.47` Dead-letter and quarantine handling (`P2`, `done`)
  - Quarantine records that fail schema validation, serialization, permission checks, or destination writes.
  - Provide dead-letter stream/dataset outputs with redaction and retention controls.
  - Allow replay from quarantine after schema or connector fixes.
  - Implemented in `services/connector-management-service/internal/models/dead_letter_quarantine.go`, `services/connector-management-service/internal/models/dead_letter_quarantine_test.go`, `services/connector-management-service/internal/repo/repo.go`, `services/connector-management-service/internal/repo/migrations/20260514000002_dead_letter_quarantine.sql`, `services/connector-management-service/internal/handlers/handlers.go`, `services/connector-management-service/internal/handlers/handlers_test.go`, `services/connector-management-service/internal/server/server.go`, `apps/web/src/lib/api/data-connection.ts`, `apps/web/src/lib/api/data-connection.test.ts`, and `apps/web/src/routes/data-connection/SourceDetailPage.tsx` with two new tables (`sync_dead_letter_sinks` and `quarantined_records`), a typed `DeadLetterSink` per sync (kind `dataset`/`stream`, target RID, retention days 1–365, redaction rules with replacement or SHA-256 hashing), a `QuarantinedRecord` model with the four required failure categories (`schema_validation`, `serialization`, `permission_check`, `destination_write`, plus `unknown` as a safety bucket), and pure helpers `ClassifyQuarantineFailure`, `ApplyDeadLetterRedaction` (immutable deep clone + dot-path + `header.*` targeting), `BuildQuarantineSummary`, `BuildQuarantineReplayPlan`, and `QuarantineExpiryFor`. The handler surface adds `GET/PUT /api/v1/data-connection/syncs/{sync_id}/dead-letter`, `GET /api/v1/data-connection/syncs/{sync_id}/quarantine`, `POST /api/v1/data-connection/syncs/{sync_id}/quarantine` (runtime endpoint that auto-classifies the error and redacts before persisting), and `POST /api/v1/data-connection/syncs/{sync_id}/quarantine:replay` (returns a structured `QuarantineReplayPlan` and only marks non-expired records). Retention is enforced both at read time (`expires_at` filtering during replay planning) and via a `PurgeExpiredQuarantinedRecords` maintenance method. The Source Detail Syncs tab renders a per-sync `DeadLetterQuarantinePanel` with sink configuration (kind/RID/retention), an editable redaction-rules table (add/remove/hash toggle), a category-counts header, and an inline records table that selects records to replay. Frontend helpers (`classifyQuarantineFailure`, `quarantineFailureCategoryLabel`, `validateDeadLetterSink`, `buildQuarantineReplayPlanLocal`, `quarantineExpiresWithin`) mirror the backend so the UI can preview before submitting.
  - Docs: [Streams core concepts](https://www.palantir.com/docs/foundry/data-integration/streams/), [Data Connection overview](https://www.palantir.com/docs/foundry/data-connection/overview).

### Governance, auditing, and compliance

- [ ] `SDC.48` Checkpoint and justification hooks (`P2`, `blocked`)
  - Require user justification before sensitive exports, external transforms with Foundry inputs, webhook execution with attachments, or source credential changes.
  - Integrate with OpenFoundry security/governance checkpoint semantics once implemented.
  - Docs: [External transforms](https://www.palantir.com/docs/foundry/data-connection/external-transforms), [Exports overview](https://www.palantir.com/docs/foundry/data-connection/export-overview/).

- [ ] `SDC.49` Sensitive data and export policy propagation (`P2`, `blocked`)
  - Propagate dataset markings or OpenFoundry-native security labels through syncs, virtual table materializations, external transforms, exports, and webhooks.
  - Enforce exportable marking/organization policy on all paths that combine Foundry data with external systems.
  - Block until security/governance label semantics are stable.
  - Docs: [External transforms](https://www.palantir.com/docs/foundry/data-connection/external-transforms), [Exports overview](https://www.palantir.com/docs/foundry/data-connection/export-overview/).

- [ ] `SDC.50` Full connection audit trail (`P2`, `todo`)
  - Emit immutable audit events for source CRUD, credential updates, network policy changes, permission changes, exploration, sync/export/webhook creation, run execution, code imports, and virtual registration.
  - Filter audit by source, user, capability, external endpoint, output resource, branch, marking/export policy, and time window.
  - Docs: [Data Connection core concepts](https://www.palantir.com/docs/foundry/data-connection/core-concepts/), [External transforms](https://www.palantir.com/docs/foundry/data-connection/external-transforms).

- [ ] `SDC.51` Resource cleanup and retention (`P2`, `todo`)
  - Identify unused sources, stale credentials, disconnected agents, abandoned sync outputs, stopped streaming exports, inactive webhooks, orphaned virtual tables, and old run logs.
  - Provide cleanup plans that preserve lineage, audit, and downstream resource warnings.
  - Docs: [Data Connection overview](https://www.palantir.com/docs/foundry/data-connection/overview), [Virtual tables](https://www.palantir.com/docs/foundry/data-integration/virtual-tables/).

## Milestone D: Magritte-style connector framework with coordinator

> **Added 2026-05-17.** The connectors above describe per-connector
> capability. This milestone adds the **connector framework** itself
> (coordinator + agent runtime + connector SDK) so new connectors are
> first-class plugins rather than per-service code, matching Palantir's
> Magritte plane.

### Coordinator and agent

- [ ] `SDC.52` Connector coordinator service (`P1`, `todo`)
  - Dedicated coordinator that registers connector kinds, validates source configs against connector schemas, dispatches sync/export/webhook work to agents or worker pools, and tracks per-source health.
  - Coordinator is the single API surface for `services/connector-management-service` to publish source actions; per-connector adapters remain pluggable.
  - Docs: [Connector framework overview](https://palantir.com/docs/foundry/data-connection/connector-framework), [Magritte coordinator concepts](https://palantir.com/docs/foundry/data-connection/coordinator).

- [ ] `SDC.53` Downloadable agent for in-network sources (`P1`, `todo`)
  - Provide a binary (Go) the customer can install on a host inside their network; agent registers with the coordinator using a service token and pulls work over a persistent outbound connection (no inbound port).
  - Agent advertises capabilities (which connectors it supports, network reach, host metadata) and accepts only work dispatched by the coordinator.
  - Docs: [Agents](https://palantir.com/docs/foundry/data-connection/set-up-agent).

- [ ] `SDC.54` Agent ↔ coordinator transport (`P1`, `todo`)
  - mTLS-secured outbound long-poll or gRPC stream from agent to coordinator; coordinator never originates connections to the agent.
  - Standard heartbeat, version reporting, and graceful drain on agent shutdown.
  - Docs: [Agents](https://palantir.com/docs/foundry/data-connection/set-up-agent).

### Connector SDK

- [ ] `SDC.55` Connector SDK contract (`P1`, `todo`)
  - Versioned Go (and optionally Python) SDK with required interfaces: `Capabilities()`, `ValidateConfig(cfg)`, `TestConnection(cfg, creds)`, `Explore(cfg, creds, path)`, `SyncFiles(...)`, `SyncTable(...)`, `SyncStream(...)`, `ExportFiles(...)`, `ExportTable(...)`, `ExportStream(...)`, `InvokeWebhook(...)`.
  - Optional interfaces for CDC, schema inference, push ingestion.
  - SDK ships with a conformance harness that any new connector must pass.
  - Docs: [Connector framework overview](https://palantir.com/docs/foundry/data-connection/connector-framework).

- [ ] `SDC.56` Built-in connectors implemented against the SDK (`P1`, `todo`)
  - Migrate REST v2, JDBC, S3, SFTP, Kafka, GCS, Azure Blob, SAP, Salesforce, and Databricks adapters to the new SDK; remove the existing per-connector `stubRunner` / `ErrNotImplemented` defaults.
  - Each connector publishes a capability matrix that the coordinator surfaces in the source setup wizard.
  - Docs: [Connector reference](https://palantir.com/docs/foundry/data-connection/connectors).

### Credentials and egress

- [ ] `SDC.57` Coordinator-managed credentials (`P1`, `todo`)
  - Coordinator stores per-source credentials in the secret store (Vault Transit by default) and serves them to agents only at dispatch time over the mTLS channel.
  - Per-credential rotation policy with audit emission on read.
  - Docs: [Credentials](https://palantir.com/docs/foundry/data-connection/credentials).

- [ ] `SDC.58` Egress policies enforced by coordinator (`P1`, `todo`)
  - Coordinator refuses to dispatch work whose target violates the source's egress policy; agents independently re-validate against the policy snapshot they receive.
  - Audit per refusal with reason code.
  - Docs: [Egress policies](https://palantir.com/docs/foundry/data-connection/egress).

### Operability

- [ ] `SDC.59` Agent fleet view (`P2`, `todo`)
  - Admin UI listing agents with version, last heartbeat, supported connectors, in-flight work, and queue depth.
  - One-click "drain" to stop dispatching new work without killing in-flight syncs.
  - Docs: [Agents](https://palantir.com/docs/foundry/data-connection/set-up-agent).

- [ ] `SDC.60` Connector marketplace publication (`P2`, `todo`)
  - Third-party connectors built against the SDK can be packaged as Marketplace products with capability declarations; install registers the connector with the coordinator.
  - Docs: [Connector framework overview](https://palantir.com/docs/foundry/data-connection/connector-framework).

## Implementation inventory to collect before coding

- [ ] `INV.1` Identify existing OpenFoundry connector/source models, REST source models, credential stores, network policy resources, and source permission models.
- [ ] `INV.2` Identify existing connector-management-service APIs, generated OpenAPI routes, frontend Data Connection routes, and SDK clients.
- [ ] `INV.3` Identify existing dataset-versioning-service transaction, branch, schema, file, table-read, and view primitives used by sync outputs.
- [ ] `INV.4` Identify existing pipeline-build-service job, build, schedule, logs, retry, and lineage integration points for sync and export jobs.
- [ ] `INV.5` Identify existing stream, queue, Kafka-compatible, hot-buffer, cold-storage, checkpoint, offset, and consumer primitives.
- [ ] `INV.6` Identify existing CDC metadata, key-by, deletion-marker, ordering-column, archive-view, and Ontology indexing primitives.
- [ ] `INV.7` Identify existing virtual table and external query pushdown abstractions, including unsupported worker/network combinations.
- [ ] `INV.8` Identify existing webhook source/action integration, request builder, parameter mapping, output extraction, invocation history, and side-effect policy code.
- [ ] `INV.9` Identify existing external transform/source import support in Python, TypeScript, sidecars, lightweight compute, and compute modules.
- [ ] `INV.10` Identify existing export resources, export controls, schedule integration, table/file/stream destination writers, and replay semantics.
- [ ] `INV.11` Identify existing agent registration, heartbeat, versioning, proxy routing, and private-network test fixtures.
- [ ] `INV.12` Identify existing Data Health, notifications, audit, retention, and security/governance primitives for connection operations.
- [ ] `INV.13` Produce a machine-readable parity matrix sibling JSON after inventory, following the pattern of [foundry-feature-parity-matrix.json](./foundry-feature-parity-matrix.json).

## Suggested service boundaries

> **Reader note (2026-05-14)** — The services in the table below are
> *target* decomposition proposals, not a current inventory of
> binaries. Some have been built under consolidated names after S8
> (`marketplace-service` → `federation-product-exchange-service`;
> `approvals-service` → `workflow-automation-service/internal/approvals`;
> `ontology-security-service` → `authorization-policy-service`;
> `ai-service` → `agent-runtime-service` + `llm-catalog-service`).
> Others are not yet implemented. For the canonical list of binaries
> on disk today, see
> [`docs/architecture/services-and-ports.md`](../architecture/services-and-ports.md).

| Surface | Responsibilities |
| --- | --- |
| `connector-management-service` | Source CRUD, connector registry, capability registry, credentials, worker selection, network policy attachment, exploration, source permissions, code import settings. |
| `data-sync-service` | Batch syncs, file syncs, table syncs, CDC sync setup, sync runs, source checkpoints, output dataset/stream commits, sync history. |
| `ingestion-replication-service` | Stream resource model, hot buffer, cold/archive storage, schema, checkpoints, consistency mode, push ingestion, replay, lag metrics, streaming sync/export runtime. |
| `virtualization service` | Virtual table registration, external schema discovery, query pushdown, update detection, virtual lineage, virtual media handoff. |
| `export-service` | File/table/streaming exports, export modes, destination writers, export schedules, export history, replay behavior, destination schema validation. |
| `webhook service` | REST API source webhooks, request builder, parameter mapping, output extraction, invocation history, action/function integration. |
| `agent service` | Agent registration, heartbeats, agent proxy policy routing, agent capability compatibility, agent health. |
| `pipeline-build-service` | Build/schedule integration for syncs/exports, logs, retries, build reports, lineage edges, job status. |
| `dataset-versioning-service` | Output datasets, transactions, branches, schemas, file manifests, stream archive datasets, virtual table dataset-like views. |
| `security/governance service` | Export controls, markings/org labels, source roles, checkpoints/justification, audit policy, credential access controls. |
| `data-health service` | Health checks for sources, agents, syncs, streams, exports, webhooks, virtual tables, CDC, lag, checkpoint failures, and schema drift. |
| `apps/web` | Data Connection UI, source setup wizard, sync/export/webhook builders, stream detail pages, virtual table registration UI, agents page, health/history panels. |

## Acceptance criteria for first complete Streaming and Data Connection milestone

- [ ] A user can create a REST API source, configure credentials and egress, test the connection, and create a webhook with typed input/output parameters.
- [ ] A user can create a filesystem/blob source, explore files, create a file sync, run it, and see output dataset transactions with file/row/byte counts.
- [ ] A user can create a table source, browse schemas/tables, create a table batch sync, infer schema, run it, and inspect history/logs.
- [ ] A user can create a stream, configure a streaming sync from a supported source, start/stop it, inspect hot/cold records, and see checkpoint state.
- [ ] A user can push records into a stream through an authenticated push ingestion endpoint and read the records from stream live view.
- [ ] A user can configure a CDC sync with primary key, ordering column, and deletion column; live and archive views show expected changelog/current-state behavior.
- [ ] A user can register a virtual table, query it, see source/lineage/update-detection metadata, and use it as a supported pipeline input.
- [ ] A user can create file, table, and streaming exports where supported, run or start them, and inspect export history.
- [ ] Source imports into code enforce source export controls before a transform can combine Foundry inputs with external system access.
- [ ] Data Health surfaces source connection failures, stale syncs, agent offline state, stream lag, checkpoint failures, webhook failures, and export failures.
- [ ] Permission checks prevent unauthorized users from viewing secrets, invoking sources, exporting Foundry data, or reading output datasets/streams.
- [ ] All OpenFoundry runtime UI is OpenFoundry-native and does not use Palantir branding, screenshots, icons, fonts, or proprietary assets.

## Test plan expectations

- Unit tests for source validation, connector capability compatibility, worker/network compatibility, credential redaction, source permission decisions, sync mode validation, stream checkpoint transitions, CDC resolution, export mode validation, webhook parameter extraction, and export-control policy evaluation.
- API tests for source CRUD, connection tests, exploration, credentials, egress policy attachment, sync CRUD/run/history, stream CRUD/live/archive reads, push ingestion, CDC syncs, virtual tables, exports, webhooks, agents, source code imports, and health checks.
- Integration tests for file sync to dataset transactions, table sync schema inference, streaming sync checkpoint restart, push ingestion to hot/cold stream views, CDC archive resolution, virtual table pipeline input, table export validation, streaming export replay behavior, and webhook action integration.
- E2E tests for source setup wizard, source exploration, file sync creation/run, streaming sync start/stop, CDC setup, virtual table registration, export setup/history, webhook setup/invocation, source import governance, and Data Connection health dashboards.
- Regression tests proving secrets are never exposed, aborted/failed sync outputs are not visible as committed dataset views, branch-only stream writes do not leak to main, unauthorized virtual table reads are blocked, and export controls cannot be bypassed through code imports or webhooks.
