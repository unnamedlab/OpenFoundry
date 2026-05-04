# Closure Global Stub Allowlist

Date: 2026-05-03

Scope: final closure search for:

```sh
rg -n 'TODO|pending|noop|LoggingWorkflowClient|ErrNotImplemented' \
  services libs workers-go infra/k8s \
  -g '*.rs' -g '*.go' -g '*.yaml' -g '*.yml' -g '*.toml'
```

Current raw result after the cleanup in this closure pass: **346 hits**.
Those hits are not accepted as a blanket pass. Each hit must map to one
of the allowlist rows below, or the closure fails.

## Runtime Blockers Fixed

| Path | Previous classification | Owner | Resolution |
|---|---|---|---|
| `libs/storage-abstraction/src/signed_urls.rs` | runtime blocker | Storage/media platform | `presigned_upload_url` and `presigned_download_url` no longer return `Ok(String::new())`; unsupported signing is now an explicit `StorageError::Unsupported`, with tests. |
| `services/media-sets-service/src/domain/storage.rs` | comment over a runtime fallback | Media sets owner | Removed the stale "pending signer" wording. The media service treats unsupported native signing as a dev/test deterministic URL path, not a successful empty URL. |
| `services/event-streaming-service/src/main.rs` | runtime blocker if deployed with no hot buffer/runtime store | Data engine owner | `NoopHotBuffer` and memory-only runtime durability remain local/dev fallbacks only. `EVENT_STREAMING_REQUIRE_REAL_BACKENDS=true` or `OPENFOUNDRY_DEPLOYMENT_ENVIRONMENT=staging|stage|prod|production` now fail fast when Kafka/NATS or Cassandra runtime durability is missing. |
| `services/event-streaming-service/src/runtime/flink/sql.rs` | runtime string emitted a `TODO` marker | Data engine owner | CEP is now surfaced as an explicit unsupported-shape warning instead of a `TODO` comment in rendered SQL. |

## Allowlist

| ID | Classification | Scope covered | Owner | Justification |
|---|---|---|---|---|
| GLS-A01 | vendor/CRD permitido | `infra/k8s/platform/charts/spark-operator/crds/sparkoperator.k8s.io_{sparkapplications,scheduledsparkapplications}.yaml` `TODO` text | Platform SRE + Spark operator chart owner | These are upstream CRD OpenAPI descriptions vendored with the Spark operator chart. They are not OpenFoundry runtime stubs. |
| GLS-A02 | vendor/comment permitido | `infra/k8s/platform/charts/mimir-distributed/{values,small,large,capped-small,capped-large}.yaml` comment text containing `pending` | Observability/SRE | Vendored chart comments and capacity notes. No OpenFoundry handler or runtime path is gated by them. |
| GLS-A03 | estado/metric name legítimo | Cassandra dashboards/service monitors and NATS Prometheus rules: `mcac_compaction_pending_tasks`, `nats_consumer_num_pending`, alert text with `pending` | Observability/SRE + Data platform | These are upstream metric names or alert labels for backlog/lag. They describe runtime state, not unfinished code. |
| GLS-A04 | estado de dominio legítimo | Domain status values and counters: approvals `pending`, OAuth `pending_auth`, ingestion `RuntimeState::pending`, marketplace install/promotion gate `pending`, federation/nexus sync/audit `pending`, CDC `pending_resolutions`, global branch `pending_reviews`, entity resolution `pending_review`, identity migration/control-panel statuses, pipeline build/schedule pending states, ontology rule queue `pending`, dataset-quality `pending_transaction_count` | Owning service teams for each domain | `pending` is part of public/domain state machines, DB columns, API responses, or metrics. Removing it would break semantics. |
| GLS-A05 | dev/test-only permitido | `libs/storage-abstraction::repositories::noop`, `libs/ontology-kernel::stores::Stores::in_memory`, handler unit tests using `noop::InMemory*`, `libs/authz-cedar::with_noop_audit`, `services/ontology-query-service`/`object-database-service`/ontology handler local in-memory stores, `infra/k8s/bench/ontology-bench-namespace.yaml` `bench.execute.noop` | Ontology platform + Test infra owners | These are in-memory fakes for unit tests, smoke tests, local dev, or benchmark no-op workloads. They are not accepted as production storage backends. |
| GLS-A06 | dev/local dry-run permitido | `libs/temporal-client::LoggingWorkflowClient`, `NoopWorkflowClient` run IDs/tests, references from workflow adapters | Workflow/Temporal maintainers | The Temporal facade already fails fast when `TEMPORAL_REQUIRE_REAL_CLIENT=true` or deployment env is staging/prod. The logging client is local dry-run only. |
| GLS-A07 | dev/local degraded-mode permitido | `services/event-streaming-service::NoopHotBuffer` and its log strings | Data engine owner | Kept for smoke/dev when no broker exists. After this pass it fails fast in staging/prod-like environments, so it cannot silently drop production events. |
| GLS-A08 | comentario/documentación permitido | Rust doc comments mentioning consolidation `pending`, dependency wording such as "without depending on", and non-runtime explanation comments | Platform architecture + owning service team | These hits explain migration/consolidation status or module boundaries. They do not execute as stubs. |
| GLS-A09 | runtime control-flow legítimo | `std::future::pending::<()>()` in `#[cfg(not(unix))]` shutdown handlers | Owning service team | This is the standard never-resolving future for the non-Unix branch of graceful shutdown selection. It is not a business operation placeholder. |

## Reproducible Verification

Raw search, expected to return the allowlisted noise above:

```sh
rg -n 'TODO|pending|noop|LoggingWorkflowClient|ErrNotImplemented' \
  services libs workers-go infra/k8s \
  -g '*.rs' -g '*.go' -g '*.yaml' -g '*.yml' -g '*.toml'
```

Residual check, expected output: `0`.

```sh
rg -n 'TODO|pending|noop|LoggingWorkflowClient|ErrNotImplemented' \
  services libs workers-go infra/k8s \
  -g '*.rs' -g '*.go' -g '*.yaml' -g '*.yml' -g '*.toml' \
| rg -v '^infra/k8s/platform/charts/spark-operator/crds/' \
| rg -v '^infra/k8s/platform/charts/mimir-distributed/' \
| rg -v '^infra/k8s/(platform/manifests/)?cassandra/' \
| rg -v '^infra/k8s/platform/observability/prometheus-rules/nats.yaml:' \
| rg -v '^infra/k8s/bench/' \
| rg -v '/tests/' \
| rg -v ':[0-9]+:\s*(//!|///|//|#)' \
| rg -v '(pending_schema_reviews|pending_re_run|pending_transaction_count|pending_upgrade_count|pending_review_count|pending_reviews|pending_resolutions|pending_schedules|pending_count|pending_events|pending_auth|oauth_pending_auth|PENDING_AUTH|pending_justification|pending_review|approved_pending_manual_apply|is_pending_status|IngestJobRuntimeState::pending|RuntimeState::pending|"pending"|\bpending\b|Pending|PENDING)' \
| rg -v '(LoggingWorkflowClient|NoopWorkflowClient|noop-|"noop"|bench.execute.noop|with_noop_audit|repositories::noop|noop::InMemory|noop::\{|pub mod noop|use noop::|noop hot buffer|NATS hot buffer unavailable|noop must succeed|_noop|noop_)' \
| wc -l
```

Sharper runtime-stub gate, also expected output: `0`.

```sh
rg -n 'ErrNotImplemented|todo!\(|unimplemented!\(|not implemented yet|implementation pending|substrate stub|Ok\(String::new\(\)\)|-- TODO:' \
  services libs workers-go infra/k8s \
  -g '*.rs' -g '*.go' -g '*.yaml' -g '*.yml' -g '*.toml' \
  --glob '!infra/k8s/platform/charts/spark-operator/crds/**' \
  --glob '!**/tests/**' \
| wc -l
```

Closure rule: any future non-zero residual must be fixed or added here
with classification, owner, and justification before formal closure can
claim the global stub search is green.
