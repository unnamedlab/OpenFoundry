# Phase 6 migration inventory

State: **2026-05-08**. This inventory is the reconciliation point between
Rust package roots (`services/*/Cargo.toml`, `libs/*/Cargo.toml`) and the Go
workspace (`openfoundry-go/services/*`, `openfoundry-go/libs/*`). It replaces
older notes that listed already-existing Go services as "not yet ported".

## Status vocabulary

| Status | Meaning |
|---|---|
| **ported** | A Go package root exists and owns the Rust contract surface at the functional/API level. Follow-up work may still improve production adapters, tests, or runtime hardening. |
| **ported but config-gated** | A Go package root exists and routes/handlers are present, but one or more production paths require explicit runtime configuration (for example Python sidecar, Iceberg catalog, backing filesystem, Kafka/Cassandra/Vespa/OpenSearch wiring). |
| **compatible-placeholder because Rust also is placeholder** | The Rust package is intentionally a shell or marker crate, and the Go counterpart mirrors that low/no-behavior contract without claiming nonexistent behavior. |
| **pending real** | Rust has meaningful behavior for which no Go package root or agreed exclusion exists. |
| **excluded by decision** | The package is intentionally not a literal Rust-to-Go port, usually because the runtime remains Rust/Scala or because the Go directory is a support/shim package rather than a Rust counterpart. |

## Service parity matrix

| Service | Rust `services/*/Cargo.toml` | Go `openfoundry-go/services/*` | Status | Notes |
|---|---:|---:|---|---|
| `agent-runtime-service` | yes | yes | **ported** | Go service exists; ai-kernel handler substrate is now present. |
| `ai-evaluation-service` | yes | yes | **ported** | Go service exists; evaluation handler path is covered by the Go port. |
| `ai-sink` | yes | yes | **ported but config-gated** | Runtime is ported; Iceberg writer remains an explicit production-adapter gate. |
| `application-composition-service` | yes | yes | **ported** | Go service exists for the Rust shell/service contract. |
| `audit-compliance-service` | yes | yes | **ported** | Go service exists with consolidated compliance domains. |
| `audit-sink` | yes | yes | **ported but config-gated** | Runtime is ported; Iceberg writer remains an explicit production-adapter gate. |
| `authorization-policy-service` | yes | yes | **ported** | Go service is the canonical active implementation for the Rust shell plus absorbed domains. |
| `code-repository-review-service` | yes | yes | **ported** | Formerly listed as port-ready; Go foundation now exists. |
| `connector-management-service` | yes | yes | **ported** | Data-connection/connector surface exists in Go. |
| `dataset-versioning-service` | yes | yes | **ported but config-gated** | Route/handler surface is present; backing filesystem/catalog flows require configured adapters. |
| `edge-gateway-service` | yes | yes | **ported** | Edge proxy and route table are Go-native. |
| `entity-resolution-service` | yes | yes | **ported** | Former architecture+slice candidate now has a Go service root. |
| `federation-product-exchange-service` | yes | yes | **ported** | Route audit reports implemented routes for this service. |
| `iceberg-catalog-service` | yes | yes | **ported** | Go foundation exists. |
| `identity-federation-service` | yes | yes | **ported** | OIDC, SAML, SCIM, Cedar, JWKS rotation, MFA/WebAuthn are Go-native. |
| `ingestion-replication-service` | yes | yes | **ported** | Go foundation exists. |
| `lineage-service` | yes | yes | **ported** | Former architecture+slice candidate now has a Go service root. |
| `llm-catalog-service` | yes | yes | **ported** | Go service exists; ai-kernel substrate is present. |
| `media-sets-service` | yes | yes | **ported** | Go foundation exists. |
| `media-transform-runtime-service` | yes | yes | **ported** | Former port-ready item now has a Go service root. |
| `model-catalog-service` | yes | yes | **ported** | Go service exists; ai-kernel substrate is present. |
| `model-deployment-service` | yes | yes | **ported** | Go service exists; ml-kernel substrate is present. |
| `notebook-runtime-service` | yes | yes | **ported but config-gated** | pyo3 work migrated through the approved Python sidecar pattern; runtime requires sidecar configuration. |
| `notification-alerting-service` | yes | yes | **ported** | Phase 2 cluster service exists in Go. |
| `object-database-service` | yes | yes | **ported** | Full HTTP foundation plus object/link store abstractions exist in Go. |
| `ontology-actions-service` | yes | yes | **ported but config-gated** | pyo3 work migrated through the approved Python sidecar pattern; runtime requires sidecar configuration. |
| `ontology-definition-service` | yes | yes | **ported** | Go foundation exists. |
| `ontology-exploratory-analysis-service` | yes | yes | **ported** | Former architecture+slice candidate now has a Go service root. |
| `ontology-indexer` | yes | yes | **ported but config-gated** | Worker surface exists; production search/Kafka adapters are runtime wiring concerns. |
| `ontology-query-service` | yes | yes | **ported but config-gated** | Query surface exists; Cassandra/NATS invalidation wiring is config-dependent. |
| `pipeline-build-service` | yes | yes | **ported but config-gated** | Route audit reports implemented routes; Python sidecar and Iceberg/log/Spark adapters are config-gated. |
| `pipeline-runner` | no Rust Cargo.toml (Scala runner) | yes | **excluded by decision** | Go directory is a compatibility/support wrapper for a non-Rust Spark runner, not a Rust-service port. |
| `reindex-coordinator-service` | yes | yes | **ported but config-gated** | Go coordinator exists; Kafka/Cassandra runtime adapters require deployment config. |
| `retrieval-context-service` | yes | yes | **ported** | Go service exists; ai-kernel substrate is present. |
| `sdk-generation-service` | yes | yes | **ported** | Phase 2 cluster service exists in Go. |
| `solution-design-service` | yes | yes | **ported** | Go service exists; ai-kernel substrate is present. |
| `sql-bi-gateway-service` | yes | yes | **excluded by decision** | DataFusion/Arrow push-down remains Rust by architecture decision; Go directory is not a literal replacement of that engine. |
| `telemetry-governance-service` | yes | yes | **ported** | Phase 2 cluster service exists in Go. |
| `tenancy-organizations-service` | yes | yes | **ported** | Workspace/tenancy surface is Go-native. |
| `workflow-automation-service` | yes | yes | **ported** | Former architecture+slice candidate now has a Go service root. |
| `template` | no | yes | **excluded by decision** | Go-only scaffold, not a Rust package. |

**Pending real services:** none at the package-root level. Remaining work is
adapter hardening/configuration, route deepening, or explicit language
exceptions; treat the Go directories above as already reconciled ports or explicit exclusions.

## Library parity matrix

| Library | Rust `libs/*/Cargo.toml` | Go `openfoundry-go/libs/*` | Status | Notes |
|---|---:|---:|---|---|
| `ai-kernel-go` | no Rust Cargo.toml in `libs/*` | yes | **excluded by decision** | Go support library for AI service ports; Rust counterpart is not part of the current `libs/*/Cargo.toml` comparison set. |
| `analytical-logic` | yes | yes | **ported** | Go package root exists. |
| `audit-trail` | yes | yes | **ported** | Event envelope/outbox bridge exists in Go. |
| `auth-middleware` | yes | yes | **ported** | JWT, tenant context, chi middleware exist in Go. |
| `authz-cedar` / `authz-cedar-go` | yes | yes | **ported** | Go package uses the `-go` suffix while mirroring the Rust authz Cedar role. |
| `cassandra-kernel` | yes | yes | **ported but config-gated** | Store interfaces and Go implementations exist; live Cassandra behavior depends on deployment config. |
| `core-models` | yes | yes | **ported** | IDs, errors, health, pagination, schema, markings, media references are Go-native. |
| `db-pool` | yes | yes | **ported** | pgx pool wrapper exists in Go. |
| `event-bus-control` | yes | yes | **ported but config-gated** | NATS/JetStream surface exists; live broker use is runtime-configured. |
| `event-bus-data` | yes | yes | **ported but config-gated** | Kafka surface exists; live broker use is runtime-configured. |
| `event-scheduler` | yes | yes | **ported** | Go package root exists. |
| `geospatial-core` | yes | yes | **compatible-placeholder because Rust also is placeholder** | Rust source is effectively a marker crate; Go mirrors the minimal contract. |
| `geospatial-tiles` | yes | yes | **ported** | Tile helpers exist in Go. |
| `idempotency` | yes | yes | **ported** | Store interfaces and memory/Postgres style backends exist. |
| `media-scanner` | yes | yes | **ported** | Go package root exists. |
| `ml-kernel-go` | no Rust Cargo.toml in `libs/*` | yes | **excluded by decision** | Go support library for ML service ports; Rust counterpart is not part of the current `libs/*/Cargo.toml` comparison set. |
| `observability` | yes | yes | **ported** | slog/OTel/Prometheus support exists in Go. |
| `ontology-kernel` | yes | yes | **ported** | Domain and handler substrate exists in Go. |
| `outbox` | yes | yes | **ported** | Transactional outbox helpers exist in Go. |
| `pipeline-expression` | yes | yes | **ported** | Parser/evaluator package exists in Go. |
| `plugin-sdk` | yes | yes | **compatible-placeholder because Rust also is placeholder** | Rust source is a one-line placeholder; Go mirrors that minimal marker role. |
| `proto-gen` | no | yes | **excluded by decision** | Generated Go protobuf stubs from canonical `../proto`. |
| `python-sidecar` | no | yes | **excluded by decision** | Go support library for the approved pyo3 sidecar architecture. |
| `query-engine` | yes | yes | **excluded by decision** | DataFusion/Arrow query execution remains Rust; Go package is not a literal engine replacement. |
| `saga` | yes | yes | **ported** | Saga choreography helper exists in Go. |
| `scheduling-cron` | yes | yes | **ported** | Cron parser/evaluator/DST behavior exists in Go. |
| `scheduling-linter` | yes | yes | **ported** | Go package root exists. |
| `search-abstraction` | yes | yes | **ported but config-gated** | Trait/factory/in-memory backend exists; live Vespa/OpenSearch backends are configured with consumers. |
| `state-machine` | yes | yes | **ported** | State transition helper exists in Go. |
| `storage-abstraction` | yes | yes | **ported but config-gated** | Core storage/search surfaces exist; concrete external backends are config/consumer-gated. |
| `testing` | yes | yes | **ported** | Go test fixtures/helpers exist. |
| `vector-store` | yes | yes | **ported but config-gated** | Vector interfaces/helpers exist; live backend use is configured by consumers. |

**Pending real libraries:** none for Rust `libs/*/Cargo.toml` package roots.
Go-only support libraries are listed explicitly so they are not mistaken for
unmatched Rust migration backlog.

## Operational follow-ups

The remaining migration work should be tracked as **integration/configuration**
items, not as missing package-root ports:

1. Keep `openfoundry-go/docs/migration/route-parity-audit.md` refreshed with
   `cd openfoundry-go && go run ./tools/route-audit --write docs/migration/route-parity-audit.md`.
2. Deepen config-gated adapters where production deployments need live
   Cassandra, Kafka, NATS, Iceberg, Vespa/OpenSearch, Spark, or Python sidecar
   dependencies.
3. Preserve the explicit exclusions for DataFusion/Arrow query execution and
   the Scala Spark runner unless an ADR changes those decisions.
