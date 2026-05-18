# Foundry Observability, Monitoring, AIP observability, and AI ethics 1:1 parity checklist

Date: 2026-05-17
Scope: public-docs-based parity plan for OpenFoundry's Foundry-equivalent
observability product surface — the alerting, monitoring view, debug,
AIP telemetry, and AI ethics areas grouped under `Observability/` in
Palantir's public documentation. Covers Data Health UI and alert routes
(email / Slack / Pulse), per-resource health-check definitions with
schedules and custom expressions, cross-resource monitoring views with
configurable widgets, pipeline / function / OQL / build-log debug
surfaces, AIP per-request and per-model telemetry, prompt/response
capture with redaction, model-card resources, intended-use and
sensitive-attribute declarations, bias evaluation results, and the
ethics review log with Approvals integration.

> **Scope distinction.** Data Health *rules* and *expectations* — the
> definitions of dataset freshness windows, schema constraints,
> transaction-health predicates, and the expectation expression
> language — are owned by
> [foundry-data-foundation-1to1-checklist.md](./foundry-data-foundation-1to1-checklist.md).
> This file owns the **product surface** that consumes those rules:
> the alerting UI, monitoring views, debug consoles, and telemetry
> retention. Compute accounting (CPU-seconds, GB-hours, dollars) is
> owned by
> [foundry-resource-management-1to1-checklist.md](./foundry-resource-management-1to1-checklist.md);
> this file owns the *observability* of those same compute jobs
> (latency, error rate, queue depth, log search), not their billing.

This document is intentionally implementation-oriented. It does not
attempt to clone Palantir branding, private source code, proprietary
assets, or any non-public behavior. The target is **functional parity
based on public Palantir Foundry documentation**.

## Parity scope boundary

All checklist work is governed by the
[Foundry public-docs parity policy](../reference/foundry-public-docs-parity-policy.md).
OpenFoundry may implement behavior described in public Palantir
documentation, but contributors must not copy private source, decompile
bundles, import tenant-specific exports, use Palantir branding, or
reuse proprietary assets.

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
| `P0` | Required for credible observability: Data Health page, basic health-check definition + schedule, one monitoring view with widgets, AIP per-request log. |
| `P1` | Required for Foundry-style parity: multi-channel alerting (Pulse / email / Slack), custom check expressions, pipeline / function / OQL debug surfaces, per-model AIP telemetry, model-card resource. |
| `P2` | Advanced parity: cross-org monitoring views, redaction in prompt/response capture, bias-evaluation pipelines, alert routing rules with on-call rotations. |

## Official Palantir documentation library

### Observability overview

- [Observability overview](https://www.palantir.com/docs/foundry/observability/overview)

### Monitoring (Data Health, health checks, monitoring views)

- [Data Health](https://www.palantir.com/docs/foundry/monitoring/data-health)
- [Health checks](https://www.palantir.com/docs/foundry/monitoring/health-checks)
- [Monitoring views](https://www.palantir.com/docs/foundry/monitoring/monitoring-views)

### Debugging

- [Debugging](https://www.palantir.com/docs/foundry/observability/debugging)
- [Pipeline debugging](https://www.palantir.com/docs/foundry/observability/pipeline-debugging)

### AIP observability

- [AIP observability](https://www.palantir.com/docs/foundry/aip/aip-observability)

### AI ethics and governance

- [AI ethics and governance](https://www.palantir.com/docs/foundry/aip/ai-ethics-and-governance)

## Milestone A: minimum viable observability and monitoring parity

### Data Health product surface

- [ ] `OBS.1` Data Health page (`P0`, `todo`)
  - A per-dataset Data Health page that surfaces the latest freshness status, schema status, transaction status, expectation pass/fail counts, and the timestamp of the last evaluation.
  - Page reads health records emitted by the data-foundation expectation engine; it does not re-evaluate rules.
  - Each row links to the rule definition (owned by data foundation) and to the latest failing sample.
  - Docs: [Data Health](https://www.palantir.com/docs/foundry/monitoring/data-health).

- [ ] `OBS.2` Per-dataset health scorecard (`P0`, `todo`)
  - Compact scorecard widget showing aggregate status (`healthy` / `degraded` / `failing` / `unknown`) with a single-glance color and the last-evaluation timestamp.
  - Embeddable in Workshop, the dataset header, and project landing pages.
  - Docs: [Data Health](https://www.palantir.com/docs/foundry/monitoring/data-health).

- [ ] `OBS.3` Alert route bootstrap: email (`P0`, `todo`)
  - Dataset owners can attach an email alert route to any Data Health rule with a single recipient list and a minimum severity threshold.
  - Delivery uses the platform notification path; deduplication suppresses repeats within a configurable cool-down.
  - Docs: [Data Health](https://www.palantir.com/docs/foundry/monitoring/data-health).

### Health checks

- [ ] `OBS.4` Health check resource (`P0`, `todo`)
  - CRUD a `health_check` resource attached to a target RID (dataset, pipeline, ontology object type, function, or agent). Fields: name, owner, target RID, check kind, schedule, severity, mute window, last status.
  - Resource is governed by the standard authorization-policy-service permissions; deny by default.
  - Docs: [Health checks](https://www.palantir.com/docs/foundry/monitoring/health-checks).

- [ ] `OBS.5` Built-in check kinds (`P0`, `todo`)
  - First-class kinds: `dataset_freshness`, `dataset_schema`, `transaction_success_rate`, `pipeline_completion`, `function_error_rate`, `agent_step_failure_rate`, `ontology_object_type_load`.
  - Each kind has a typed parameters schema (e.g. freshness window, error-rate threshold + window).
  - Docs: [Health checks](https://www.palantir.com/docs/foundry/monitoring/health-checks).

- [ ] `OBS.6` Check schedule (`P0`, `todo`)
  - Schedules: `cron`, `interval`, `on_event` (e.g. on transaction commit), `manual`.
  - Per-tenant scheduler enforces a maximum frequency floor and a per-owner budget; over-budget schedules are throttled with a typed warning.
  - Docs: [Health checks](https://www.palantir.com/docs/foundry/monitoring/health-checks).

- [ ] `OBS.7` Check result history (`P0`, `todo`)
  - Every evaluation writes a `check_result` row: timestamp, status, observed value, threshold, error message (when applicable), and a sample payload reference when relevant.
  - History page shows the last N results with a timeline; raw rows queryable via API for downstream alerting.
  - Docs: [Health checks](https://www.palantir.com/docs/foundry/monitoring/health-checks).

### Monitoring views

- [ ] `OBS.8` Monitoring view resource (`P0`, `todo`)
  - A `monitoring_view` resource lives in a project and references a set of target resources (datasets, pipelines, object types, functions, agents) plus a widget layout.
  - View permissions follow the project; widgets re-check viewer permissions before rendering data.
  - Docs: [Monitoring views](https://www.palantir.com/docs/foundry/monitoring/monitoring-views).

- [ ] `OBS.9` Configurable widgets (`P0`, `todo`)
  - Built-in widgets: `latency` (with selectable p50/p95/p99), `error_rate`, `throughput`, `queue_depth`, `freshness`, `health_summary`, `check_history`, `recent_failures`.
  - Each widget binds to a target RID + time window; widgets re-query on view open and on refresh.
  - Docs: [Monitoring views](https://www.palantir.com/docs/foundry/monitoring/monitoring-views).

- [ ] `OBS.10` Saved views and refresh schedule (`P0`, `todo`)
  - Users can save a view, set a default time window, configure auto-refresh interval (off / 30s / 1m / 5m / 15m), and pin to project landing.
  - Docs: [Monitoring views](https://www.palantir.com/docs/foundry/monitoring/monitoring-views).

### AIP observability bootstrap

- [ ] `OBS.11` Per-request AIP log (`P0`, `todo`)
  - For each AIP function / agent call, persist a `aip_request_log` row: request id, actor, function/agent id, model id, prompt token count, completion token count, latency, status, parent trace id.
  - Read access governed by project permissions; logs queryable via API and surfaced on the function page.
  - Docs: [AIP observability](https://www.palantir.com/docs/foundry/aip/aip-observability).

- [ ] `OBS.12` AIP function page telemetry tab (`P0`, `todo`)
  - Each Logic / function detail page has a Telemetry tab showing request count, error count, average latency, total tokens for the selected window.
  - Tab embeds the same widgets as monitoring views for visual consistency.
  - Docs: [AIP observability](https://www.palantir.com/docs/foundry/aip/aip-observability).

## Milestone B: credible Foundry-style observability parity

### Multi-channel alerting

- [ ] `OBS.13` Pulse alert route (`P1`, `todo`)
  - Alert routes can target a Pulse channel (reusing the notification surface in the Foundry shell); routing respects per-user mute windows.
  - Docs: [Data Health](https://www.palantir.com/docs/foundry/monitoring/data-health).

- [ ] `OBS.14` Slack alert route (`P1`, `todo`)
  - Outbound Slack route via a tenant-configured webhook or app token; payload includes target resource, status, last-known value, and a link back to the check page.
  - Failures retry with exponential backoff; per-route failure counter is surfaced in the alert-route admin page.
  - Docs: [Data Health](https://www.palantir.com/docs/foundry/monitoring/data-health).

- [ ] `OBS.15` Mute and snooze (`P1`, `todo`)
  - Per-check or per-route mute windows (one-off or recurring) prevent firing during planned maintenance; mutes record actor and reason and surface in audit.
  - Docs: [Health checks](https://www.palantir.com/docs/foundry/monitoring/health-checks).

- [ ] `OBS.16` Escalation to Pulse (`P1`, `todo`)
  - When a check has been failing past a configurable escalation threshold, escalate from informational notification to a Pulse incident requiring acknowledgement.
  - Docs: [Health checks](https://www.palantir.com/docs/foundry/monitoring/health-checks).

### Custom check expressions

- [ ] `OBS.17` Custom check expression language (`P1`, `todo`)
  - A `custom` health-check kind accepts a typed expression (subset of the expectation expression language defined in data foundation) evaluated against the target's recent records, metrics, or query result.
  - Compile-time validation rejects unbounded scans; per-evaluation timeout and row-limit enforced.
  - Docs: [Health checks](https://www.palantir.com/docs/foundry/monitoring/health-checks).

- [ ] `OBS.18` Sample-row capture on failure (`P1`, `todo`)
  - When a check fails, capture a redacted sample of the offending rows / records and link it from the result; sample respects the target's markings and restricted views.
  - Docs: [Health checks](https://www.palantir.com/docs/foundry/monitoring/health-checks).

### Debug surfaces

- [ ] `OBS.19` Pipeline debug page (`P1`, `todo`)
  - Per-build debug page exposes per-node logs, per-node timing, queued / running / finished state transitions, and a sample-row capture per node when permitted.
  - Page is reachable from the build header and from any failing health check that references the build.
  - Docs: [Pipeline debugging](https://www.palantir.com/docs/foundry/observability/pipeline-debugging).

- [ ] `OBS.20` Per-node profiling (`P1`, `todo`)
  - Lightweight profile per node: rows-in, rows-out, peak memory, total CPU-seconds, partition skew indicator. Surfaced as a sortable table on the debug page.
  - Docs: [Pipeline debugging](https://www.palantir.com/docs/foundry/observability/pipeline-debugging).

- [ ] `OBS.21` Function debug surface (`P1`, `todo`)
  - For each Function / Logic invocation: request trace id, input bindings (with redaction), intermediate block outputs, audit events emitted, latency breakdown, and downstream call list.
  - Permitted users can re-run the same inputs in a sandbox from the debug page.
  - Docs: [Debugging](https://www.palantir.com/docs/foundry/observability/debugging).

- [ ] `OBS.22` Ontology query debug (`P1`, `todo`)
  - For OQL / object-set queries: query plan tree, estimated vs. observed row counts per stage, index usage, marking-filter cost, total cost units, and cache hits.
  - Page is reachable from the failing query, from Object Explorer slow-query banner, and from monitoring views.
  - Docs: [Debugging](https://www.palantir.com/docs/foundry/observability/debugging).

- [ ] `OBS.23` Build log search (`P1`, `todo`)
  - Full-text search across pipeline build logs scoped to projects the user can see; supports time-window filters, log-level filters, and pinning of frequent queries.
  - Docs: [Pipeline debugging](https://www.palantir.com/docs/foundry/observability/pipeline-debugging).

### AIP per-model telemetry

- [ ] `OBS.24` Per-model latency percentiles (`P1`, `todo`)
  - For each AIP model in the registry, emit and surface p50 / p95 / p99 latency over rolling windows; per-model dashboard groups by tenant and project.
  - Docs: [AIP observability](https://www.palantir.com/docs/foundry/aip/aip-observability).

- [ ] `OBS.25` Per-function cost telemetry (`P1`, `todo`)
  - Per-function rollup of token spend (prompt + completion + cached), per-model and per-day, reconciled against the resource-management compute-account dataset (this file owns observation, not billing).
  - Docs: [AIP observability](https://www.palantir.com/docs/foundry/aip/aip-observability).

- [ ] `OBS.26` Error breakdown by error class (`P1`, `todo`)
  - AIP requests carry typed error classes (`model_timeout`, `model_overloaded`, `policy_denied`, `tool_failed`, `validation_failed`, `unknown`); per-model error-class chart available on monitoring views and per-function pages.
  - Docs: [AIP observability](https://www.palantir.com/docs/foundry/aip/aip-observability).

- [ ] `OBS.27` Prompt and response capture (`P1`, `todo`)
  - Per-request prompt and response capture stored in a project-scoped sink with the same markings as the function's project; capture is opt-in per function and bounded by a per-tenant retention window.
  - Docs: [AIP observability](https://www.palantir.com/docs/foundry/aip/aip-observability).

- [ ] `OBS.28` Eval-run telemetry (`P1`, `todo`)
  - Each evaluation suite run emits an `eval_run` summary with per-evaluator metrics and per-test-case outcomes; surface in the function's Telemetry tab and from monitoring views.
  - Docs: [AIP observability](https://www.palantir.com/docs/foundry/aip/aip-observability).

- [ ] `OBS.29` Agent step traces (`P1`, `todo`)
  - For agents (multi-step LLM flows), capture a step-by-step trace: tool calls, intermediate prompts, observed outputs, retries, and termination reason. Trace viewer lives in the function debug surface.
  - Docs: [AIP observability](https://www.palantir.com/docs/foundry/aip/aip-observability).

### AI ethics and governance

- [ ] `OBS.30` Model-card resource (`P1`, `todo`)
  - CRUD a `model_card` resource per registered AIP model: provider, version, training-data summary, supported modalities, known limitations, evaluation summary, ethics-review status, approved use scope.
  - Model-card is required before a model is exposed to AIP functions in production; missing card blocks deployment with a typed error.
  - Docs: [AI ethics and governance](https://www.palantir.com/docs/foundry/aip/ai-ethics-and-governance).

- [ ] `OBS.31` Intended-use declaration (`P1`, `todo`)
  - Each model-card and each AIP function carries an `intended_use` declaration; mismatch at call time (e.g. calling a model from a function whose intended-use exceeds the model's approved scope) is logged and optionally blocked per project policy.
  - Docs: [AI ethics and governance](https://www.palantir.com/docs/foundry/aip/ai-ethics-and-governance).

## Milestone C: advanced parity

### Cross-org monitoring views

- [ ] `OBS.32` Cross-org monitoring view (`P2`, `todo`)
  - For organizations with multi-org tenants, an admin-only monitoring view can aggregate widgets across orgs the viewer is permitted to see; rows missing permission are silently dropped (deny-by-default).
  - Docs: [Monitoring views](https://www.palantir.com/docs/foundry/monitoring/monitoring-views).

- [ ] `OBS.33` Share-as-snapshot (`P2`, `todo`)
  - Permitted users can snapshot a monitoring view to an immutable, time-stamped artifact (for incident reviews) with the viewer's permission set baked in; snapshots are read-only and audit-tracked.
  - Docs: [Monitoring views](https://www.palantir.com/docs/foundry/monitoring/monitoring-views).

- [ ] `OBS.34` Workshop embed (`P2`, `todo`)
  - A Workshop widget can embed a saved monitoring view (or a single widget from it) and inherits the viewer's permissions at render time.
  - Docs: [Monitoring views](https://www.palantir.com/docs/foundry/monitoring/monitoring-views).

### Redaction and capture controls

- [ ] `OBS.35` Redaction in prompt/response capture (`P2`, `todo`)
  - Per-project redaction policies (regex, named entities, marking-driven) applied to captured prompts/responses before storage; original payload never persisted.
  - Per-call audit records redaction policy id and number of redactions; downstream consumers see only redacted captures.
  - Docs: [AIP observability](https://www.palantir.com/docs/foundry/aip/aip-observability).

- [ ] `OBS.36` Capture retention windows (`P2`, `todo`)
  - Per-tenant configurable retention for prompt/response capture (default 30 days, max bounded by platform policy); expired rows hard-deleted on a scheduled job with an audit summary.
  - Docs: [AIP observability](https://www.palantir.com/docs/foundry/aip/aip-observability).

### Bias evaluation and ethics review

- [ ] `OBS.37` Sensitive-attribute declaration (`P2`, `todo`)
  - Model-cards and AIP functions can declare sensitive attributes (e.g. protected demographic columns) that must not be used as model inputs; runtime check rejects calls that bind those attributes unless an override is granted by ethics review.
  - Docs: [AI ethics and governance](https://www.palantir.com/docs/foundry/aip/ai-ethics-and-governance).

- [ ] `OBS.38` Bias evaluation pipeline (`P2`, `todo`)
  - Reusable evaluation pipeline that takes a model + a labeled fairness dataset and reports per-group metrics (e.g. demographic parity, equalized odds); results attach to the model-card and to the function.
  - Docs: [AI ethics and governance](https://www.palantir.com/docs/foundry/aip/ai-ethics-and-governance).

- [ ] `OBS.39` Ethics review log (`P2`, `todo`)
  - Every model-card status transition (`draft` -> `review` -> `approved` / `rejected` / `restricted`) is recorded with reviewer, comment, and links to evidence; log is queryable and pinned to the model-card.
  - Docs: [AI ethics and governance](https://www.palantir.com/docs/foundry/aip/ai-ethics-and-governance).

- [ ] `OBS.40` Integration with Approvals (`P2`, `todo`)
  - Model-card approval can require a structured Approvals flow (see security/governance checklist); approvals issue a signed token referenced by the model-card; revocation flips the card to `restricted` and blocks new function deployments.
  - Docs: [AI ethics and governance](https://www.palantir.com/docs/foundry/aip/ai-ethics-and-governance).

### Alert routing and on-call

- [ ] `OBS.41` Alert routing rules (`P2`, `todo`)
  - Routing-rule resource: matches on resource type / project / severity / time-of-day and dispatches to one or more routes (email, Slack, Pulse, PagerDuty-style webhook).
  - Rule evaluation order is deterministic; conflicts produce a configuration warning.
  - Docs: [Data Health](https://www.palantir.com/docs/foundry/monitoring/data-health).

- [ ] `OBS.42` On-call rotation resource (`P2`, `todo`)
  - Per-team on-call rotation with weekly / daily shifts, holiday overrides, and one-off swaps; routing rules resolve "on-call(team)" to the current shift's recipient at fire time.
  - Docs: [Data Health](https://www.palantir.com/docs/foundry/monitoring/data-health).

- [ ] `OBS.43` Acknowledgement and incident state (`P2`, `todo`)
  - Pulse incidents support `acknowledge` / `assign` / `resolve` transitions with comments; per-incident timeline records every notification and every state change.
  - Docs: [Data Health](https://www.palantir.com/docs/foundry/monitoring/data-health).

## Implementation inventory to collect before coding

- [ ] `INV.1` Identify the current notification path used by `notification-alerting-service` (or equivalent) and how it dispatches email vs. in-app Pulse messages.
- [ ] `INV.2` Identify the dataset/job event bus that the data-foundation expectation engine writes to; the monitoring service consumes from here.
- [ ] `INV.3` Identify the existing AIP request log emission (if any) in the Logic / Functions runtime — there must be exactly one canonical emitter to avoid double-counting.
- [ ] `INV.4` Identify the build / pipeline service's log sink and search index; the build-log search feature depends on its query API.
- [ ] `INV.5` Identify the resource-management compute-account dataset shape so per-function cost telemetry can join correctly without re-implementing billing.
- [ ] `INV.6` Identify the project / marking enforcement entrypoint for any widget that re-queries data at render time.
- [ ] `INV.7` Identify how Approvals (security/governance) signs and revokes approval tokens so the model-card workflow can consume them.
- [ ] `INV.8` Produce a parity matrix sibling JSON entry once a first implementation inventory is in place.

## Suggested service boundaries

| Surface | Responsibilities |
| --- | --- |
| `monitoring-service` | Data Health page backend, health-check resource + scheduler, check-result history, monitoring-view resource + widget queries, saved-view sharing, snapshot artifacts. |
| `pipeline-debug-service` (or extend the existing build service) | Per-node log fetch, per-node profiling, sample-row capture, build-log search index, debug-page wiring for builds and OQL queries. |
| `aip-observability-service` | AIP request log ingest, per-function and per-model rollups, prompt/response capture with redaction, eval-run summaries, agent-step trace store, telemetry queries used by function pages and monitoring widgets. |
| `ethics-governance-service` | Model-card CRUD, intended-use / sensitive-attribute declarations, bias-evaluation results, ethics review log, approval-token consumption. |
| `notification-alerting-service` (reuse) | Email / Slack / Pulse dispatch, mute/snooze, routing rules, on-call rotations, incident acknowledgement. |
| `apps/web` | Data Health page, health-check editor, monitoring view editor + viewer, pipeline / function / OQL debug surfaces, AIP telemetry tabs, model-card editor, ethics review log UI. |

## Acceptance criteria

- [ ] A dataset owner can open a Data Health page for any dataset they own, see freshness / schema / transaction / expectation status, and attach an email alert route.
- [ ] A user can create a health check on a dataset, pipeline, ontology object type, function, or agent with a built-in kind and a schedule, and see check results accumulate in history.
- [ ] A user can create a monitoring view with latency, error rate, throughput, queue depth, and freshness widgets, save it, and pin it to the project landing page.
- [ ] An AIP function page shows per-request logs and a Telemetry tab with request count, error count, average latency, and total tokens.
- [ ] Alert routes can fire to Pulse and Slack with mute/snooze and escalation behavior matching the public docs.
- [ ] A custom health-check expression with a typed expression is evaluated on schedule, fails safely on bad expressions, and captures redacted sample rows on failure.
- [ ] Pipeline debug, Function debug, and OQL debug surfaces are reachable from a failing build or query and respect the viewer's permissions and markings.
- [ ] Per-model latency p50/p95/p99, per-function cost, and per-error-class breakdowns are available for AIP requests.
- [ ] A model-card resource exists for every registered AIP model, requires intended-use and sensitive-attribute declarations, and gates production deployment with an Approvals-issued token.
- [ ] All OpenFoundry runtime UI is OpenFoundry-native and does not use Palantir branding, screenshots, icons, fonts, or proprietary assets.

## Test plan expectations

- Unit tests for health-check schedule resolution, custom expression compilation, sample-row redaction, monitoring-view widget binding, AIP request log envelope validation, model-card status transitions, sensitive-attribute rejection, and alert dedupe/cool-down.
- API tests for health-check CRUD, check-result history pagination, monitoring-view CRUD + share-as-snapshot, alert-route CRUD across email / Slack / Pulse, AIP per-request log queries, per-model telemetry rollups, model-card CRUD, and ethics review log append-only semantics.
- Integration tests for end-to-end firing of a failing health check through email and Slack with mute / snooze / escalation behavior, monitoring view render under restricted views, pipeline-debug retrieval of per-node logs and profiling, function-debug re-run in sandbox, AIP request log emission from Logic and agents, and bias-evaluation pipeline writing results back to a model-card.
- E2E tests for Data Health page navigation, monitoring view creation and pinning, AIP Telemetry tab, model-card creation through Approvals to production deployment, and ethics-review-driven model restriction propagating to function deployment.
- Regression tests proving that prompt/response capture is never persisted unredacted, that markings on the target resource are enforced by widgets at render time, that custom check expressions cannot run unbounded scans, that build-log search cannot return logs from projects the viewer cannot see, and that snapshot artifacts remain immutable across re-renders.
