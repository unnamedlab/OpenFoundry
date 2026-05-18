# Foundry AIP Document Intelligence, Analyst, Model Catalog, BYOM, and AIP admin 1:1 parity checklist

Date: 2026-05-17
Scope: public-docs-based parity plan for OpenFoundry's AIP application and
admin surfaces that are not owned by the AIP Logic/Evals or AIP
Agents/Threads/Assist checklists: AIP Document Intelligence (upload, OCR,
field/table/signature/redaction extraction, batch processing, citation,
human-in-the-loop review, Ontology writeback), AIP Analyst (conversational
analyst grounded in Ontology and datasets, NL-to-OQL/SQL, multi-turn
refinement, chart generation, save-as-Workshop-page, governance), AIP
Model Catalog (registered LLM and embedding models, capability tags,
latency/cost/quota, per-org availability, default-model selection),
bring-your-own-model registration (HTTP endpoint, function interface,
OpenAI-compatible adapter, credentials, health probe, audit, region,
cost), LLM-provider compatible APIs (OpenAI-style chat-completions,
embeddings, batch, API key auth, rate limiting, OpenAPI export), LLM
capacity management (org quotas, per-feature allocation, burst budget,
throttling, alerts, dashboard), and the AIP admin surface that toggles
features per organization, selects default models per feature, manages
per-user opt-in, and gates the enrollment master switch.

This document is intentionally implementation-oriented. It does not
attempt to clone Palantir branding, private source code, proprietary
assets, screenshots, or any non-public behavior. The target is
**functional parity based on public Palantir Foundry documentation**:
the same product concepts, comparable document, analyst, catalog, BYOM,
LLM-API, capacity, and admin workflows, compatible resource models where
useful, and OpenFoundry-native implementation details that can be tested
locally.

> **Scope distinction.** AIP Logic and AIP Evals parity is owned by
> [`foundry-aip-logic-evals-1to1-checklist.md`](./foundry-aip-logic-evals-1to1-checklist.md).
> AIP Agents, AIP Threads, and AIP Assist parity is owned by
> [`foundry-aip-agents-threads-assist-1to1-checklist.md`](./foundry-aip-agents-threads-assist-1to1-checklist.md).
> Server-side hosted-model deployment lifecycle (model packaging,
> hosted inference endpoint deployment, Model Studio authoring) is
> owned by [`foundry-model-integration-model-studio-1to1-checklist.md`](./foundry-model-integration-model-studio-1to1-checklist.md).
> This file owns the AIP-side document application, the conversational
> analyst application, the model catalog / BYOM / LLM-compatible API
> surfaces, the LLM capacity management plane, and the per-organization
> AIP feature administration UI/API.

## Parity scope boundary

All checklist work is governed by the
[Foundry public-docs parity policy](../reference/foundry-public-docs-parity-policy.md).
OpenFoundry may implement behavior described in public Palantir
documentation, but contributors must not copy private source, decompile
bundles, import tenant-specific exports, use Palantir branding, or reuse
proprietary assets. The product target is functional parity in an
OpenFoundry-native implementation, not a pixel-perfect clone.

This checklist should integrate with the AIP Logic/Evals checklist for
function-backed extraction and evaluator handoffs; with the AIP
Agents/Threads/Assist checklist for conversational sessions, retrieval
traces, and citations; with the Ontology/Object Views checklist for
Ontology writeback, object/property schemas, and OQL targets; with
Data Foundation for dataset-backed analyst targets and batch document
outputs; with Media Sets for source document storage and extraction
provenance; with Developer Console for API key issuance and per-app rate
limit policy; with Resource Management for usage attribution; and with
Security/Governance for marking, audit, retention, and per-org policy.
It should not duplicate those underlying surfaces.

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
| `P0` | Required for credible demo workflows that upload a document, extract fields, ask the analyst questions over Ontology, register a model in the catalog, register a BYOM, and toggle AIP for an organization. |
| `P1` | Required for Foundry-style Document Intelligence, Analyst, Model Catalog, BYOM, LLM-API, capacity, and AIP-admin parity beyond simple single-doc/single-question runs. |
| `P2` | Advanced governance, human-in-the-loop review, cross-org sharing, capability probing, OpenAI-compatible adapter mode, per-feature usage dashboards, cost attribution, and enterprise rollout parity. |

## Official Palantir documentation library

These public docs should be treated as the external behavioral contract
while implementing this checklist.

### AIP Document Intelligence

- [AIP Document Intelligence overview](https://www.palantir.com/docs/foundry/aip-document-intelligence/overview)
- [Extract fields with AIP Document Intelligence](https://www.palantir.com/docs/foundry/aip-document-intelligence/extract-fields)
- [Review extracted documents](https://www.palantir.com/docs/foundry/aip-document-intelligence/review)

### AIP Analyst

- [AIP Analyst overview](https://www.palantir.com/docs/foundry/aip-analyst/overview)
- [Configure AIP Analyst](https://www.palantir.com/docs/foundry/aip-analyst/configure)

### AIP Model Catalog

- [AIP Model Catalog overview](https://www.palantir.com/docs/foundry/aip-model-catalog/overview)
- [Supported LLMs](https://www.palantir.com/docs/foundry/aip/supported-llms)

### Bring your own model

- [Bring your own model overview](https://www.palantir.com/docs/foundry/aip-bring-your-own-model/overview)
- [Register an LLM using function interfaces](https://www.palantir.com/docs/foundry/aip-bring-your-own-model/register-llm-using-function-interfaces)
- [Use a registered LLM](https://www.palantir.com/docs/foundry/aip-bring-your-own-model/use-registered-llm)

### LLM-provider compatible APIs

- [LLM-provider compatible APIs](https://www.palantir.com/docs/foundry/aip/llm-provider-compatible-apis)

### LLM capacity management

- [LLM capacity management](https://www.palantir.com/docs/foundry/administration/llm-capacity-management)
- [Compute usage with AIP](https://www.palantir.com/docs/foundry/aip/compute-usage-with-aip)

### Enable AIP features

- [Enable AIP features](https://www.palantir.com/docs/foundry/administration/enable-aip-features)

## Target OpenFoundry resource model

The implementation should define stable OpenFoundry-owned resources that
can map to public Foundry concepts without requiring Palantir RID
formats. Compatibility aliases may be accepted at service boundaries,
but persisted state should use OpenFoundry canonical IDs.

| Public Foundry concept | OpenFoundry resource target | Required notes |
| --- | --- | --- |
| Document Intelligence project | `docintel_project` | Project/folder-managed resource with name, description, doc-model selection, target schema, reviewers, ontology writeback config, batch settings, owner, permissions. |
| Document upload | `docintel_document` | Uploaded source document with storage location, mime/type, page count, OCR status, extraction status, parse errors, source permissions, retention policy. |
| Document model | `docintel_model` | Registered extraction model with capabilities (OCR, layout, table, signature, redaction), input modalities, supported page sizes, latency/cost metadata, version. |
| Target extraction schema | `docintel_target_schema` | Typed field schema describing structured output: scalar fields, table fields, signature flags, checkbox/boolean flags, redaction zones, and validation rules. |
| Extraction result | `docintel_extraction` | Per-document extraction output with field values, table rows, citations to source page/region, confidence scores, model/version, run id. |
| Review task | `docintel_review_task` | Human-in-the-loop queue entry with status (pending/assigned/approved/rejected/edited), assignee, reviewer comments, edited values, audit trail. |
| Document batch | `docintel_batch` | Batch processing job over many documents with concurrency, retry policy, progress, partial failure handling, output dataset reference. |
| Analyst session | `aipan_session` | Conversational analyst session with selected ontology/dataset targets, message history, generated queries, charts, citations, governance scope. |
| Analyst query | `aipan_query` | Generated NL-to-OQL/NL-to-SQL artifact with source NL prompt, target language, compiled query, validation status, execution result reference. |
| Analyst chart | `aipan_chart` | Generated chart specification with type, encodings, data reference, persistence target, save-as-Workshop-page metadata. |
| Analyst governance scope | `aipan_governance_scope` | Configuration listing queryable ontology object types, properties, saved object sets, datasets, and forbidden columns/markings per analyst session. |
| Catalog model | `mcat_model` | Registered LLM/embedding model entry with provider, family, version, capability tags, latency/cost/quota, per-org availability, default-flag-per-feature. |
| Catalog capability tag | `mcat_capability` | Tag attached to a catalog model: `text_gen`, `embeddings`, `vision`, `tool_use`, `structured_output`, `long_context`, `streaming`, etc. |
| Catalog quota policy | `mcat_quota` | Per-model latency/cost/quota policy: max RPM, max TPM, max concurrent, max tokens per request, soft/hard budgets per org/project. |
| BYOM registration | `byom_registration` | Externally-hosted LLM registered by HTTP endpoint or function interface, including adapter type, auth method, region, capability probe results, health status, audit. |
| BYOM credential | `byom_credential` | Encrypted credential record for a BYOM endpoint, with rotation metadata, last-used timestamp, owner, and access-control policy. |
| BYOM adapter | `byom_adapter` | Adapter contract translating OpenFoundry's internal LLM request shape to/from the BYOM endpoint (OpenAI-compatible, Anthropic-compatible, function-interface, generic). |
| LLM-compatible API app | `llmapi_app` | Developer Console-style external application that owns API keys, rate limits, scope, and OpenAPI export for the LLM-compatible HTTP surface. |
| LLM-compatible API key | `llmapi_key` | API key tied to an app with scope (chat-completions, embeddings, batch), rate limit, last-used metadata, rotation, audit. |
| LLM-compatible batch job | `llmapi_batch` | Asynchronous batch inference job with input dataset, output dataset, status, partial failure handling, retention policy. |
| LLM capacity pool | `cap_pool` | Organization-level capacity allocation across AIP features (Logic, Threads, Assist, Document Intelligence, Analyst), with burst budget and throttle policy. |
| Capacity allocation | `cap_allocation` | Per-feature allocation row within a capacity pool with reserved/burst limits, current utilization, throttle behavior, alert thresholds. |
| Capacity alert | `cap_alert` | Threshold-driven alert (75%/90%/100%/burst-exhausted) with channel routing and acknowledgement state. |
| AIP feature toggle | `aip_feature_toggle` | Per-organization toggle for each AIP feature with admin actor, change timestamp, justification, and enrollment-master-switch dependency. |
| Per-user AIP opt-in | `aip_user_opt_in` | Per-user opt-in/out record for a given AIP feature with default-model selection, opt-in source, and audit. |
| AIP usage record | `aip_usage_record` | Per-invocation usage row attributed to feature, model, user, project, app, with token/compute/cost metadata used by dashboards and cost reports. |

## Milestone A: minimum viable AIP document, analyst, catalog, and admin parity

### AIP Document Intelligence basics

- [ ] `AIPX.1` Document Intelligence project CRUD (`P0`, `todo`)
  - Create, list, get, update metadata, duplicate, archive/delete, and restore Document Intelligence projects in project folders.
  - Track name, description, owner, doc-model selection, default target schema, reviewers, and permissions.
  - Docs: [AIP Document Intelligence overview](https://www.palantir.com/docs/foundry/aip-document-intelligence/overview).

- [ ] `AIPX.2` Document upload and storage handoff (`P0`, `todo`)
  - Upload native PDFs and supported document formats into a configured storage/media set location.
  - Record mime/type, page count, source permissions, parse status, and retention policy on the `docintel_document` record.
  - Reject unsupported formats with actionable error messages and link the document to its Document Intelligence project.
  - Docs: [AIP Document Intelligence overview](https://www.palantir.com/docs/foundry/aip-document-intelligence/overview).

- [ ] `AIPX.3` OCR and layout extraction (`P0`, `todo`)
  - Run a configurable OCR + layout pass over uploaded documents to produce text, blocks, lines, words, and bounding boxes per page.
  - Persist OCR results with model/version metadata, confidence per region, and a permission-aware preview.
  - Surface OCR failures (corrupt page, unsupported language, image-only PDF) as structured errors on the document record.
  - Docs: [AIP Document Intelligence overview](https://www.palantir.com/docs/foundry/aip-document-intelligence/overview), [Extract fields with AIP Document Intelligence](https://www.palantir.com/docs/foundry/aip-document-intelligence/extract-fields).

- [ ] `AIPX.4` Doc-model selection (`P0`, `todo`)
  - Allow project owners to choose from a list of registered document models with capability tags (OCR, layout, table, signature, redaction, structured-output).
  - Validate that the selected model supports the project's target schema (e.g. structured-output requirement, table-extraction requirement).
  - Docs: [Extract fields with AIP Document Intelligence](https://www.palantir.com/docs/foundry/aip-document-intelligence/extract-fields).

- [ ] `AIPX.5` Target extraction schema authoring (`P0`, `todo`)
  - Define typed extraction schemas with scalar fields, optional/required flags, default values, regex validators, and per-field documentation.
  - Persist the schema as a versioned resource referenced by Document Intelligence projects.
  - Docs: [Extract fields with AIP Document Intelligence](https://www.palantir.com/docs/foundry/aip-document-intelligence/extract-fields).

- [ ] `AIPX.6` Field extraction with citations (`P0`, `todo`)
  - Run the selected doc-model against an uploaded document with the project's target schema and emit a `docintel_extraction` record.
  - For each extracted field, include the value, confidence, source page number, and source region (bounding box) so the UI can highlight the originating text.
  - Show extractions side-by-side with the source PDF in the Document Intelligence project UI.
  - Docs: [Extract fields with AIP Document Intelligence](https://www.palantir.com/docs/foundry/aip-document-intelligence/extract-fields).

### AIP Analyst basics

- [ ] `AIPX.7` Analyst session shell (`P0`, `todo`)
  - Provide a conversational analyst UI with a session list, message history, NL prompt input, generated query preview, and result panel.
  - Persist sessions per user and per project with selected governance scope and message log.
  - Docs: [AIP Analyst overview](https://www.palantir.com/docs/foundry/aip-analyst/overview).

- [ ] `AIPX.8` NL-to-OQL / NL-to-SQL generation (`P0`, `todo`)
  - Generate OQL (for ontology targets) or SQL (for dataset targets) from a natural-language prompt, using a model from the AIP Model Catalog.
  - Validate the generated query against the session's governance scope before showing results: only whitelisted object types, properties, datasets, and columns may be referenced.
  - Show the generated query to the user with copy-as-code and "explain this query" affordances.
  - Docs: [AIP Analyst overview](https://www.palantir.com/docs/foundry/aip-analyst/overview), [Configure AIP Analyst](https://www.palantir.com/docs/foundry/aip-analyst/configure).

- [ ] `AIPX.9` Analyst query execution with citations (`P0`, `todo`)
  - Execute the validated generated query against Ontology/Object Storage V2 or the dataset query layer using the caller's permissions.
  - Return results as a table preview with up to N rows and emit citations to the underlying object set, dataset, or query.
  - Capture errors (permission denied, ambiguous join, type mismatch) as structured turn results.
  - Docs: [AIP Analyst overview](https://www.palantir.com/docs/foundry/aip-analyst/overview).

### AIP Model Catalog basics

- [ ] `AIPX.10` Model Catalog list and detail (`P0`, `todo`)
  - List registered LLM and embedding models with provider, family, version, capability tags, region availability, and default-model flags.
  - Provide a per-model detail page showing capability tags, latency/cost/quota policy, supported features, and per-org availability.
  - Docs: [AIP Model Catalog overview](https://www.palantir.com/docs/foundry/aip-model-catalog/overview), [Supported LLMs](https://www.palantir.com/docs/foundry/aip/supported-llms).

- [ ] `AIPX.11` Default-model selection per feature (`P0`, `todo`)
  - Allow an admin to designate a default model for each AIP feature (Logic, Threads, Assist, Document Intelligence, Analyst) within an organization.
  - Persist the selection on `aip_feature_toggle` rows and expose the resolved default through the AIP runtime.
  - Reject default-model selection for a feature when the selected model lacks the required capability tag.
  - Docs: [AIP Model Catalog overview](https://www.palantir.com/docs/foundry/aip-model-catalog/overview), [Enable AIP features](https://www.palantir.com/docs/foundry/administration/enable-aip-features).

### Bring your own model basics

- [ ] `AIPX.12` Register an LLM by HTTP endpoint (`P0`, `todo`)
  - Allow admins to register an externally-hosted LLM by HTTP endpoint, selecting an adapter type (OpenAI-compatible, Anthropic-compatible, generic).
  - Capture endpoint URL, auth method (bearer/header/oauth), region pin, owner, and a name/description.
  - Store credentials in an encrypted credential record separate from the registration metadata.
  - Docs: [Bring your own model overview](https://www.palantir.com/docs/foundry/aip-bring-your-own-model/overview), [Use a registered LLM](https://www.palantir.com/docs/foundry/aip-bring-your-own-model/use-registered-llm).

### Enable AIP features admin basics

- [ ] `AIPX.13` Enrollment-level AIP master switch (`P0`, `todo`)
  - Provide an enrollment-level master toggle that enables or disables all AIP product surfaces (Logic, Threads, Assist, Document Intelligence, Analyst, Model Catalog) globally.
  - Default to OFF on first install; flipping ON unlocks per-organization toggles but does not implicitly enable any feature.
  - Audit the master-switch change with actor, timestamp, and justification.
  - Docs: [Enable AIP features](https://www.palantir.com/docs/foundry/administration/enable-aip-features).

- [ ] `AIPX.14` Per-organization AIP feature toggles (`P0`, `todo`)
  - Allow org admins to toggle each AIP feature independently (Logic, Threads, Assist, Document Intelligence, Analyst, Model Catalog, BYOM, LLM-compatible APIs).
  - Block toggling ON when the enrollment master switch is OFF or when no compatible default model is selected.
  - Persist per-feature change history with actor, timestamp, prior state, and reason.
  - Docs: [Enable AIP features](https://www.palantir.com/docs/foundry/administration/enable-aip-features).

- [ ] `AIPX.15` AIP admin UI shell (`P0`, `todo`)
  - Add an OpenFoundry-native AIP admin page reachable from the Control Panel that shows the enrollment master switch, per-org feature toggles, per-feature default-model selectors, and a feature-status summary.
  - Gate access by an `aip-admin` role and audit every visit / change.
  - Docs: [Enable AIP features](https://www.palantir.com/docs/foundry/administration/enable-aip-features).

## Milestone B: credible Foundry-style AIP application parity

### Document Intelligence enhanced extraction

- [ ] `AIPX.16` Table extraction (`P1`, `todo`)
  - Detect tables in source documents, infer header/data rows, and emit table-shaped fields with per-cell citations.
  - Support multi-page tables with continuation detection and a configurable max-table-size guardrail.
  - Validate extracted tables against the target schema's table field definitions (column types, required columns).
  - Docs: [Extract fields with AIP Document Intelligence](https://www.palantir.com/docs/foundry/aip-document-intelligence/extract-fields).

- [ ] `AIPX.17` Batch document processing (`P1`, `todo`)
  - Run a Document Intelligence project against a list of documents (dataset, folder, media set) as a batch job.
  - Support configurable concurrency, retry policy on transient errors, partial-failure handling, and progress reporting.
  - Emit a batch summary with per-document status, an aggregate confidence histogram, and a re-run-failed action.
  - Docs: [Extract fields with AIP Document Intelligence](https://www.palantir.com/docs/foundry/aip-document-intelligence/extract-fields).

- [ ] `AIPX.18` Ontology writeback integration (`P1`, `todo`)
  - Map extracted fields to an Ontology object type's properties and stage Ontology edits through the existing action engine.
  - Require an `apply_action` action type for the writeback and never write to the Ontology directly from the extraction service.
  - Show a preview of staged edits per document with diff against any existing object before commit.
  - Docs: [Extract fields with AIP Document Intelligence](https://www.palantir.com/docs/foundry/aip-document-intelligence/extract-fields), [Review extracted documents](https://www.palantir.com/docs/foundry/aip-document-intelligence/review).

### Analyst advanced workflows

- [ ] `AIPX.19` Multi-turn refinement (`P1`, `todo`)
  - Allow the analyst to take follow-up turns that refine prior queries (filter, group, sort, additional joins) while preserving context.
  - Carry result schema, governance scope, and prior generated query into the next NL-to-OQL/SQL invocation.
  - Render a per-turn query diff so users can see how their request changed the underlying query.
  - Docs: [AIP Analyst overview](https://www.palantir.com/docs/foundry/aip-analyst/overview).

- [ ] `AIPX.20` Chart generation (`P1`, `todo`)
  - Generate chart specifications (bar, line, scatter, pie, stacked) from a NL prompt plus the prior query's result schema.
  - Render the chart inline in the analyst panel and expose chart type / encoding controls for manual override.
  - Persist generated charts on the `aipan_chart` resource with reference to the originating session/turn.
  - Docs: [AIP Analyst overview](https://www.palantir.com/docs/foundry/aip-analyst/overview).

- [ ] `AIPX.21` Save analyst result as Workshop page (`P1`, `todo`)
  - Save a chart or query result from the analyst into a Workshop module as a new page or widget.
  - Map the analyst session's selected object sets / dataset references into Workshop variables.
  - Preserve provenance from the saved Workshop page back to the originating analyst session for audit.
  - Docs: [AIP Analyst overview](https://www.palantir.com/docs/foundry/aip-analyst/overview), [Configure AIP Analyst](https://www.palantir.com/docs/foundry/aip-analyst/configure).

### Model Catalog capability and quota

- [ ] `AIPX.22` Capability tags and feature compatibility (`P1`, `todo`)
  - Persist capability tags on each catalog model: `text_gen`, `embeddings`, `vision`, `tool_use`, `structured_output`, `long_context`, `streaming`.
  - Use capability tags to filter models in pickers across AIP surfaces (Logic LLM block, Agent Studio, Threads, Document Intelligence, Analyst).
  - Reject feature configurations that select a model missing a required capability tag (e.g. Document Intelligence requiring `vision` + `structured_output`).
  - Docs: [AIP Model Catalog overview](https://www.palantir.com/docs/foundry/aip-model-catalog/overview), [Supported LLMs](https://www.palantir.com/docs/foundry/aip/supported-llms).

- [ ] `AIPX.23` Latency, cost, and quota metadata (`P1`, `todo`)
  - Capture per-model published latency (P50/P95 per modality), cost (input/output token unit price, image unit price), and quota policy (RPM/TPM/concurrent).
  - Surface latency/cost/quota in the model picker, in feature-config UIs, and in usage dashboards.
  - Docs: [AIP Model Catalog overview](https://www.palantir.com/docs/foundry/aip-model-catalog/overview).

- [ ] `AIPX.24` Per-organization model availability (`P1`, `todo`)
  - Allow admins to gate which catalog models are available per organization (e.g. exclude a model that violates a data residency policy).
  - Enforce availability at request time: feature invocations using a disallowed model are rejected with a structured `model_unavailable` error.
  - Docs: [AIP Model Catalog overview](https://www.palantir.com/docs/foundry/aip-model-catalog/overview), [Enable AIP features](https://www.palantir.com/docs/foundry/administration/enable-aip-features).

### BYOM via function interface and adapter

- [ ] `AIPX.25` Register an LLM via function interface (`P1`, `todo`)
  - Allow registering an LLM whose implementation is a published Function (TypeScript/Python) that implements the standardized `LLM` function interface.
  - Validate the function signature matches the interface contract (chat-completions, optional embeddings, optional tool-use protocol).
  - Make the registered function-backed LLM addressable from the AIP Model Catalog like any other model.
  - Docs: [Register an LLM using function interfaces](https://www.palantir.com/docs/foundry/aip-bring-your-own-model/register-llm-using-function-interfaces).

- [ ] `AIPX.26` OpenAI-compatible adapter mode (`P1`, `todo`)
  - Support an OpenAI-compatible adapter that translates OpenFoundry's internal LLM request shape to/from the OpenAI `chat/completions` and `embeddings` API contracts.
  - Validate that the BYOM endpoint speaks the OpenAI dialect (sampling probe on first registration).
  - Surface translation errors (e.g. unsupported tool-use shape) as structured BYOM health findings.
  - Docs: [Bring your own model overview](https://www.palantir.com/docs/foundry/aip-bring-your-own-model/overview), [Use a registered LLM](https://www.palantir.com/docs/foundry/aip-bring-your-own-model/use-registered-llm).

- [ ] `AIPX.27` BYOM credential management (`P1`, `todo`)
  - Store BYOM credentials encrypted at rest with rotation metadata, last-used timestamps, and per-key access-control policy.
  - Allow admin-driven rotation, expiry, and revocation; revoked credentials cause future invocations to fail closed.
  - Audit credential reads, writes, rotations, and uses.
  - Docs: [Bring your own model overview](https://www.palantir.com/docs/foundry/aip-bring-your-own-model/overview).

### LLM-compatible HTTP API gateway

- [ ] `AIPX.28` Chat-completions HTTP endpoint (`P1`, `todo`)
  - Expose an OpenAI-style `/v1/chat/completions` HTTP endpoint serving Foundry-hosted models that an external app may call.
  - Authenticate via an API key tied to a Developer Console app and enforce the app's model allowlist + scope.
  - Stream responses via server-sent events when the upstream model supports it.
  - Docs: [LLM-provider compatible APIs](https://www.palantir.com/docs/foundry/aip/llm-provider-compatible-apis).

- [ ] `AIPX.29` Embeddings HTTP endpoint (`P1`, `todo`)
  - Expose an OpenAI-style `/v1/embeddings` HTTP endpoint serving Foundry-hosted embedding models.
  - Validate input shape (string, array of strings, token arrays where supported) and emit per-input vector + token-count metadata.
  - Docs: [LLM-provider compatible APIs](https://www.palantir.com/docs/foundry/aip/llm-provider-compatible-apis).

- [ ] `AIPX.30` Per-app rate limiting (`P1`, `todo`)
  - Apply per-app RPM/TPM/concurrent-request rate limits using a token-bucket or sliding-window algorithm.
  - Return standard `429` responses with `Retry-After` headers when the limit is exceeded.
  - Surface real-time rate-limit utilization per app in the Developer Console admin view.
  - Docs: [LLM-provider compatible APIs](https://www.palantir.com/docs/foundry/aip/llm-provider-compatible-apis).

### LLM capacity management basics

- [ ] `AIPX.31` Organization-level capacity pool (`P1`, `todo`)
  - Provision a per-organization capacity pool denominated in tokens-per-minute or AIP compute units, with a documented default and admin override.
  - Show current utilization, soft-warning threshold, and hard-stop threshold on the AIP admin UI.
  - Docs: [LLM capacity management](https://www.palantir.com/docs/foundry/administration/llm-capacity-management), [Compute usage with AIP](https://www.palantir.com/docs/foundry/aip/compute-usage-with-aip).

- [ ] `AIPX.32` Per-feature capacity allocation (`P1`, `todo`)
  - Allow admins to reserve a portion of the org capacity pool for each AIP feature (Logic, Threads, Assist, Document Intelligence, Analyst, BYOM, LLM-API gateway).
  - Enforce per-feature reservations at request time; once a feature's reservation is exhausted it falls back to the shared burst budget if available.
  - Persist allocation changes with audit, actor, and reason.
  - Docs: [LLM capacity management](https://www.palantir.com/docs/foundry/administration/llm-capacity-management).

- [ ] `AIPX.33` Burst budget and throttling behavior (`P1`, `todo`)
  - Configure a shared burst budget that any feature may consume after its reservation runs out, subject to a documented overshoot cap.
  - When the burst budget is exhausted, throttle requests with a structured `capacity_exhausted` error containing the cooldown window and the feature that exceeded.
  - Provide a documented retry behavior so SDKs/agents can back off correctly.
  - Docs: [LLM capacity management](https://www.palantir.com/docs/foundry/administration/llm-capacity-management).

## Milestone C: advanced parity

### Document Intelligence advanced

- [ ] `AIPX.34` Signature, checkbox, and redaction detection (`P2`, `todo`)
  - Detect handwritten signatures, checkbox state (checked/unchecked/indeterminate), and redaction zones in extracted documents.
  - Emit per-detection bounding boxes, confidence scores, and citations linked to the source page region.
  - Allow the target schema to declare signature and checkbox fields that map directly onto these detections.
  - Docs: [Extract fields with AIP Document Intelligence](https://www.palantir.com/docs/foundry/aip-document-intelligence/extract-fields).

- [ ] `AIPX.35` Human-in-the-loop review queue (`P2`, `todo`)
  - Route low-confidence extractions to a review queue with assignment, due date, and priority.
  - Provide reviewers with a side-by-side source-document + extracted-fields editor with confidence chips, field-level approve/edit/reject controls, and per-field comments.
  - Persist review decisions on `docintel_review_task` with an immutable audit log; downstream Ontology writeback only proceeds for approved or edited fields.
  - Docs: [Review extracted documents](https://www.palantir.com/docs/foundry/aip-document-intelligence/review).

- [ ] `AIPX.36` Reviewer routing and SLA tracking (`P2`, `todo`)
  - Support routing tasks to specific reviewers or reviewer groups based on document model, schema, project, or marking.
  - Track SLA timers, reassignment events, and aggregate reviewer throughput per project.
  - Docs: [Review extracted documents](https://www.palantir.com/docs/foundry/aip-document-intelligence/review).

### Analyst governance and quality

- [ ] `AIPX.37` Analyst governance scope configuration (`P2`, `todo`)
  - Let admins configure per-project / per-session whitelists of queryable Ontology object types, properties, saved object sets, and datasets.
  - Block columns/properties with restrictive markings unless the caller has the required marking access.
  - Reject generated queries that touch out-of-scope resources with a structured `governance_violation` error.
  - Docs: [Configure AIP Analyst](https://www.palantir.com/docs/foundry/aip-analyst/configure).

- [ ] `AIPX.38` Confidence indicators and uncertainty surfacing (`P2`, `todo`)
  - Surface per-turn confidence indicators on generated queries based on signals such as ambiguous joins, missing context, or low-confidence semantic mappings.
  - Show inline warnings when the model is uncertain about which property/column the user meant.
  - Persist confidence telemetry for analyst quality dashboards and prompt refinement.
  - Docs: [AIP Analyst overview](https://www.palantir.com/docs/foundry/aip-analyst/overview).

- [ ] `AIPX.39` Analyst citations and audit (`P2`, `todo`)
  - Emit citations from analyst responses back to the underlying object set / dataset / query, including a permission-aware preview link.
  - Audit every analyst turn with actor, governance scope, generated query, executed query, citation list, and any reasoning trace.
  - Docs: [AIP Analyst overview](https://www.palantir.com/docs/foundry/aip-analyst/overview).

### Model Catalog advanced

- [ ] `AIPX.40` Cross-organization model sharing (`P2`, `todo`)
  - Allow an enrollment-level admin to share a registered model across multiple organizations with per-org availability and quota.
  - Preserve cost attribution per consuming organization, even when the model record is shared.
  - Audit cross-org sharing actions and revocations.
  - Docs: [AIP Model Catalog overview](https://www.palantir.com/docs/foundry/aip-model-catalog/overview).

- [ ] `AIPX.41` Capability probe and BYOM health checks (`P2`, `todo`)
  - Run an automated capability probe on registration and on a configurable schedule against BYOM endpoints to detect actual capabilities (chat, embeddings, tool-use, streaming, structured-output, vision).
  - Mark capability tags as `declared` vs `probed`; runtime selection prefers `probed` capabilities and warns when a tag is only declared.
  - Persist health-check history with latency, success rate, and recent error messages.
  - Docs: [Bring your own model overview](https://www.palantir.com/docs/foundry/aip-bring-your-own-model/overview).

### LLM-compatible APIs advanced

- [ ] `AIPX.42` Batch inference API (`P2`, `todo`)
  - Expose an asynchronous batch API for chat-completions and embeddings against Foundry-hosted models.
  - Accept input as a dataset reference or uploaded JSONL file, run inference in chunks, and emit an output dataset/file with per-row status.
  - Support partial-failure handling, retry policy, and per-batch cost/usage attribution.
  - Docs: [LLM-provider compatible APIs](https://www.palantir.com/docs/foundry/aip/llm-provider-compatible-apis).

- [ ] `AIPX.43` OpenAPI export for the LLM-compatible surface (`P2`, `todo`)
  - Publish an OpenAPI specification for the LLM-compatible API surface that external SDKs can consume.
  - Include per-endpoint examples, rate-limit headers, error envelopes, and links to authentication docs.
  - Validate that generated OpenFoundry client SDKs and any third-party OpenAI-compatible client can call the surface successfully.
  - Docs: [LLM-provider compatible APIs](https://www.palantir.com/docs/foundry/aip/llm-provider-compatible-apis).

### Capacity, usage, and admin advanced

- [ ] `AIPX.44` Capacity alert routing (`P2`, `todo`)
  - Configure threshold-driven capacity alerts (75% / 90% / 100% / burst-exhausted) with channel routing (in-app banner, email, webhook, Data Health).
  - Persist acknowledgement state per alert and require an acknowledge action before re-firing on the same threshold within a cooldown.
  - Docs: [LLM capacity management](https://www.palantir.com/docs/foundry/administration/llm-capacity-management).

- [ ] `AIPX.45` Per-feature usage dashboard (`P2`, `todo`)
  - Provide a usage dashboard split by AIP feature (Logic, Threads, Assist, Document Intelligence, Analyst, BYOM, LLM-API gateway) showing token/compute consumption, request counts, and error rates over rolling windows.
  - Drill down by model, organization, project, user, and external app.
  - Surface anomalies (sudden spike, sustained drop, error-rate elevation) as a top-of-page banner.
  - Docs: [LLM capacity management](https://www.palantir.com/docs/foundry/administration/llm-capacity-management), [Compute usage with AIP](https://www.palantir.com/docs/foundry/aip/compute-usage-with-aip).

- [ ] `AIPX.46` Cost attribution per model and per feature (`P2`, `todo`)
  - Compute per-invocation cost from per-model input/output token unit prices and emit `aip_usage_record` rows with attribution to feature, model, user, project, app, and BYOM endpoint.
  - Aggregate cost by feature, model, and organization for a billing-style report exportable as CSV/dataset.
  - Validate that BYOM endpoints with custom pricing schemas can attach a cost calculator without source modifications.
  - Docs: [Compute usage with AIP](https://www.palantir.com/docs/foundry/aip/compute-usage-with-aip).

- [ ] `AIPX.47` Per-user AIP feature opt-in (`P2`, `todo`)
  - Allow individual users to opt in/out of specific AIP features (e.g. Assist, Analyst) within the bounds of org-level enablement.
  - Persist opt-in state on `aip_user_opt_in` with default-model selection and audit.
  - Honor user opt-out at request time: invocations from opted-out users are rejected with a structured `user_opted_out` error.
  - Docs: [Enable AIP features](https://www.palantir.com/docs/foundry/administration/enable-aip-features).

- [ ] `AIPX.48` BYOM region pinning and residency enforcement (`P2`, `todo`)
  - Pin each BYOM registration to one or more permitted regions and reject invocations that would route to a non-permitted region.
  - Validate region pins against the consuming organization's data residency policy before allowing use of the BYOM in that org.
  - Audit region-pin changes and residency-violation rejections.
  - Docs: [Bring your own model overview](https://www.palantir.com/docs/foundry/aip-bring-your-own-model/overview), [LLM capacity management](https://www.palantir.com/docs/foundry/administration/llm-capacity-management).

- [ ] `AIPX.49` AIP admin audit and change history (`P2`, `todo`)
  - Audit every admin action across the AIP admin surface: master switch, per-org feature toggles, per-feature default-model changes, per-user opt-ins, capacity-pool adjustments, BYOM registrations, model availability changes, and API key issuance.
  - Provide a filterable history view with actor, timestamp, prior state, new state, justification, and downstream effects.
  - Docs: [Enable AIP features](https://www.palantir.com/docs/foundry/administration/enable-aip-features).

- [ ] `AIPX.50` Document Intelligence audit and provenance (`P2`, `todo`)
  - Audit document upload, OCR runs, extraction runs, review decisions, Ontology writebacks, and batch jobs with actor, model/version, target schema, and per-document provenance.
  - Preserve a provenance link from every Ontology object created by a Document Intelligence project back to the originating extraction, review decision, and source document region.
  - Docs: [AIP Document Intelligence overview](https://www.palantir.com/docs/foundry/aip-document-intelligence/overview), [Review extracted documents](https://www.palantir.com/docs/foundry/aip-document-intelligence/review).

- [ ] `AIPX.51` Analyst replay-for-eval (`P2`, `todo`)
  - Replay a recorded analyst session against a new model or new governance scope to produce a side-by-side comparison consumable by AIP Evals.
  - Capture deterministic seeds where the model supports them and record divergence per turn.
  - Docs: [AIP Analyst overview](https://www.palantir.com/docs/foundry/aip-analyst/overview), [Configure AIP Analyst](https://www.palantir.com/docs/foundry/aip-analyst/configure).

- [ ] `AIPX.52` LLM-compatible API key rotation and revocation (`P2`, `todo`)
  - Support API key rotation with overlap windows (old + new key valid for a configurable period) and immediate revocation.
  - Emit usage alerts when a soon-to-be-revoked key is still in heavy use and surface them in the Developer Console.
  - Audit issuance, rotation, and revocation events with actor and justification.
  - Docs: [LLM-provider compatible APIs](https://www.palantir.com/docs/foundry/aip/llm-provider-compatible-apis).

- [ ] `AIPX.53` Capacity and cost forecasting (`P2`, `todo`)
  - Forecast capacity utilization and cost based on rolling 7/30/90-day usage trends; surface "projected to exceed budget in N days" warnings on the AIP admin UI.
  - Allow admins to set per-organization soft and hard monthly budgets that trigger throttling at the hard cap.
  - Docs: [LLM capacity management](https://www.palantir.com/docs/foundry/administration/llm-capacity-management), [Compute usage with AIP](https://www.palantir.com/docs/foundry/aip/compute-usage-with-aip).

## Implementation inventory to collect before coding

- [ ] `INV.1` Identify existing OpenFoundry document storage, media-set, OCR, layout, parsing, chunking, and citation primitives that can back AIP Document Intelligence.
- [ ] `INV.2` Identify existing Ontology object/property schemas, OQL execution paths, action engine, and writeback semantics needed for Document Intelligence-to-Ontology and Analyst-to-Ontology workflows.
- [ ] `INV.3` Identify existing dataset query layer (SQL / Object Storage V2 / Trino-like) and saved object set APIs that AIP Analyst can target.
- [ ] `INV.4` Identify existing LLM provider router, model registry, prompt/token metering, streaming response, and embedding components that the Model Catalog and BYOM surfaces can build on.
- [ ] `INV.5` Identify existing credential storage, encryption, rotation, and audit primitives needed for BYOM credentials and LLM-compatible API keys.
- [ ] `INV.6` Identify existing Developer Console app, API key, OpenAPI export, and per-app rate-limit primitives that the LLM-compatible HTTP surface can reuse.
- [ ] `INV.7` Identify existing capacity / quota / throttle primitives (per-org pool, burst budget, structured 429 envelope) and decide whether they live in `edge-gateway-service`, a dedicated capacity service, or both.
- [ ] `INV.8` Identify existing usage / cost / billing dataset emission primitives (per-feature attribution, per-model rates, exportable rollups) that the AIP usage dashboards and cost reports require.
- [ ] `INV.9` Identify existing Control-Panel-style admin UI primitives, per-org toggle storage, per-user preference storage, and audit feeds that the AIP admin surface can reuse.
- [ ] `INV.10` Produce a machine-readable parity matrix sibling JSON after inventory, following the pattern of [foundry-feature-parity-matrix.json](./foundry-feature-parity-matrix.json).

## Suggested service boundaries

> **Reader note (2026-05-17)** — The services in the table below are
> *target* decomposition proposals, not a current inventory of
> binaries. Some surfaces may consolidate into existing services
> (`llm-catalog-service`, `agent-runtime-service`,
> `model-integration-service`, `edge-gateway-service`,
> `developer-console-service`, `tenancy-organizations-service`,
> `authorization-policy-service`). For the canonical list of
> binaries on disk today, see
> [`docs/architecture/services-and-ports.md`](../architecture/services-and-ports.md).

| Surface | Responsibilities |
| --- | --- |
| `aip-document-intelligence-service` | Document Intelligence project CRUD, document upload, OCR/layout, doc-model selection, target schema, field/table/signature/checkbox/redaction extraction, citation persistence, batch processing, review queue, Ontology writeback staging, audit/provenance. |
| `aip-analyst-service` | Analyst session CRUD, governance scope, NL-to-OQL/NL-to-SQL generation, multi-turn refinement, chart generation, save-as-Workshop-page, citations, confidence indicators, replay-for-eval. |
| `aip-model-catalog-service` | Catalog model registration, capability tags, latency/cost/quota metadata, per-org availability, default-model selection per feature, cross-org sharing, capability probes. |
| `llm-byom-adapter-service` | BYOM registration (HTTP endpoint, function interface), OpenAI/Anthropic/generic adapters, credential management, health checks, region pinning, cost calculators, audit. |
| `llm-compatible-api-gateway` | OpenAI-style chat-completions, embeddings, and batch endpoints; per-app authentication, rate limiting, streaming, OpenAPI export, key issuance/rotation/revocation. |
| `aip-admin-service` | Enrollment master switch, per-org feature toggles, default-model selection per feature, per-user opt-in, capacity-pool / allocation / burst / alerts, AIP usage dashboard, cost attribution rollups, AIP audit. |
| `apps/web` | AIP admin UI, Document Intelligence project editor, document viewer + extraction overlay, review queue UI, Analyst conversational UI, chart panel, save-as-Workshop-page flow, Model Catalog browser, BYOM registration wizard, LLM-compatible API admin, capacity dashboard. |

## Acceptance criteria for first complete AIP application, catalog, and admin milestone

- [ ] An admin can flip the enrollment AIP master switch and toggle Document Intelligence, Analyst, Model Catalog, and BYOM independently for an organization.
- [ ] A user can create a Document Intelligence project, upload a PDF, define a target schema, select a doc-model, and extract structured fields with citations back to source page regions.
- [ ] A user can ask the AIP Analyst a natural-language question grounded in a configured Ontology / dataset scope, see the generated OQL/SQL, run it under their own permissions, and receive cited results.
- [ ] An admin can browse the AIP Model Catalog, see capability tags / latency / cost / quota per model, and pick a default model per AIP feature within an organization.
- [ ] An admin can register an externally-hosted LLM by HTTP endpoint with an OpenAI-compatible adapter, manage credentials, and reference the BYOM from the Model Catalog like any other model.
- [ ] An external application can call `/v1/chat/completions` and `/v1/embeddings` using an API key tied to a Developer Console app, subject to per-app rate limits, with a published OpenAPI specification.
- [ ] An admin can configure org-level LLM capacity, reserve allocations per AIP feature, see a usage dashboard, and receive capacity alerts at documented thresholds.
- [ ] Document Intelligence extractions can be staged for Ontology writeback through the action engine, with a human-in-the-loop review queue gating commit when configured.
- [ ] All Document Intelligence, Analyst, Catalog, BYOM, LLM-API gateway, capacity, and admin actions are audited with actor / actor surface / before / after / justification.
- [ ] All OpenFoundry runtime UI is OpenFoundry-native and does not use Palantir branding, screenshots, icons, fonts, or proprietary assets.

## Test plan expectations

- Unit tests for Document Intelligence target-schema validation, OCR result shape, field citation construction, table extraction validation, signature/checkbox/redaction detection result shape, batch progress accounting, and review-task state transitions.
- Unit tests for Analyst governance-scope enforcement (object/property/dataset whitelist), generated query validation, multi-turn context carry, chart generation shape, and save-as-Workshop-page payload mapping.
- Unit tests for Model Catalog capability-tag filtering, latency/cost/quota persistence, per-org availability checks, and default-model selection validation against capability requirements.
- Unit tests for BYOM registration validation, adapter selection, credential encryption/rotation/revocation, capability-probe scheduling, region-pin enforcement, and cost-calculator pluggability.
- Unit tests for LLM-compatible API request shape conformance (chat-completions, embeddings, batch), per-app rate-limit accounting, key rotation / revocation behavior, and OpenAPI export drift checks.
- Unit tests for capacity-pool accounting, per-feature allocation enforcement, burst-budget consumption, structured `capacity_exhausted` envelopes, threshold-driven alert routing, and forecast computation.
- API tests for Document Intelligence project CRUD, document upload, OCR/extraction runs, batch jobs, review queue operations, and Ontology writeback staging.
- API tests for Analyst session CRUD, NL-to-OQL/NL-to-SQL turns, query execution, chart generation, save-as-Workshop-page, and replay-for-eval.
- API tests for Model Catalog list/detail/availability, default-model selection, BYOM registration / credential / health / probe, and the LLM-compatible chat-completions / embeddings / batch endpoints.
- API tests for AIP admin master switch, per-org feature toggles, per-user opt-in, capacity-pool / allocation / burst / alerts, and per-feature usage dashboard rollups.
- Integration tests for Document Intelligence-to-Ontology writeback via the action engine, Analyst grounded in Ontology + dataset targets, BYOM invocation through the Model Catalog runtime, and LLM-compatible API key issuance from Developer Console.
- Security tests for per-org model availability enforcement, BYOM region-pin enforcement, marking-aware Analyst result redaction, document-source permission propagation into extraction visibility, API-key scoping, and AIP admin role gating.
- Load tests for batch document processing under concurrency, LLM-compatible HTTP streaming under high RPS, per-app rate-limit fairness, capacity-pool throttling correctness, and usage-dashboard query latency under realistic event volume.
- Regression tests proving Document Intelligence cannot mutate the Ontology outside the action engine, Analyst-generated queries cannot escape their governance scope, BYOM credentials are never logged in plaintext, LLM-compatible API requests outside a permitted scope are rejected with structured envelopes, and capacity exhaustion never bypasses the documented throttle envelope.
