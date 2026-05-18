# 00 — Foundry-native contract for the Aviation/MRO PoC

> Goal: this PoC must be demonstrable as if it had been built directly in **Palantir Foundry**. OpenFoundry may implement the internals differently, but every user-visible step, artifact, and acceptance criterion must map to a public Foundry capability.

---

## Non-negotiable interpretation

1. **Foundry first, OpenFoundry second.** The PoC is no longer a generic OpenFoundry demo with Foundry-like labels; it is a Foundry-native workflow that OpenFoundry must emulate.
2. **Customer-facing names must be Foundry concepts.** In the demo script and UI copy, use names such as Data Connection, Dataset, Pipeline Builder, Code Repositories, Ontology Manager, Object Type, Link Type, Action Type, Workshop, Quiver, AIP Chatbot, Data Lineage, Data Health, Action Log, and Global Branching.
3. **OpenFoundry service names are implementation details.** Names such as `connector-management-service`, `dataset-versioning-service`, or `agent-runtime-service` can remain in engineering runbooks, but not as the primary narrative for the customer demo.
4. **If a Foundry capability cannot be publicly verified, mark it as an emulation.** Do not claim it is identical to Foundry unless the behavior is supported by Palantir public documentation or validated by a Palantir environment.
5. **The acceptance test is behavioral parity.** The question is not whether the OpenFoundry architecture resembles Foundry; the question is whether a Foundry practitioner would perform the same step with an equivalent Foundry resource.

---

## Public Foundry documentation baseline checked

Use these public pages as the baseline for the PoC vocabulary and acceptance criteria:

| Area | Public documentation |
|---|---|
| Data ingestion | [Data Connection — overview](https://www.palantir.com/docs/foundry/data-connection/overview) |
| Datasets | [Core concepts — datasets](https://www.palantir.com/docs/foundry/data-integration/datasets) |
| Pipelines | [Pipeline Builder — overview](https://www.palantir.com/docs/foundry/pipeline-builder/overview/) and [Transforms — overview](https://www.palantir.com/docs/foundry/pipeline-builder/transforms-overview/) |
| Data quality | [Data Health](https://www.palantir.com/docs/foundry/observability/data-health/) |
| Ontology | [Object and link types — type reference](https://www.palantir.com/docs/foundry/object-link-types/type-reference) |
| Actions | [Action rules](https://www.palantir.com/docs/foundry/action-types/rules/), [use actions in the platform](https://www.palantir.com/docs/foundry/action-types/use-actions/), [action log](https://www.palantir.com/docs/foundry/action-types/action-log), [notifications](https://www.palantir.com/docs/foundry/action-types/notifications/), and [webhooks](https://www.palantir.com/docs/foundry/action-types/webhooks) |
| Workshop | [Workshop widgets](https://www.palantir.com/docs/foundry/workshop/concepts-widgets), [Object Table](https://www.palantir.com/docs/foundry/workshop/widgets-object-table), [Button Group](https://www.palantir.com/docs/foundry/workshop/widgets-button-group/), and [Map widget](https://www.palantir.com/docs/foundry/workshop/widgets-map/) |
| Quiver | [Quiver action button](https://www.palantir.com/docs/foundry/quiver/card-action-button) |
| AIP | [AIP Chatbot Studio overview](https://www.palantir.com/docs/foundry/chatbot-studio/overview/), [AIP Chatbot tools](https://www.palantir.com/docs/foundry/agent-studio/tools/), and [retrieval context](https://www.palantir.com/docs/foundry/agent-studio/retrieval-context/) |
| Branching | [Global Branching overview](https://www.palantir.com/docs/foundry/foundry-branching/overview/) and [supported functionality](https://www.palantir.com/docs/foundry/foundry-branching/supported-functionality/) |
| Workshop branching caveat | [Branching Workshop modules](https://www.palantir.com/docs/foundry/workshop/branching-rebasing/) |

---

## Foundry-native capability mapping

| PoC capability | How it must be described in Foundry terms | OpenFoundry implementation target | Acceptance criteria |
|---|---|---|---|
| Register OpenSky, NOAA, BTS, FAA, OurAirports, and synthetic MRO sources | **Data Connection** sources and syncs into raw Foundry datasets | `connector-management-service` + `ingestion-replication-service` | Each source appears as a connection/sync; outputs are raw datasets with schema, permissions, build history, and lineage. |
| Land files and tables | **Datasets** containing structured/semi-structured files with transaction history | `dataset-versioning-service` | Every write is a transaction; users can inspect versions/builds and downstream lineage. |
| Batch transforms | **Pipeline Builder** or **Code Repositories transforms** | `pipeline-build-service` + `pipeline-runner-spark` | Transform graph is visible; runs are schedulable; failed runs expose errors and health state. |
| Streaming/OpenSky live | Foundry streaming pipeline feeding datasets or ontology-backed objects | `ingestion-replication-service` over Kafka-compatible bus | Live aircraft updates reach the dashboard within the PoC latency target without bypassing dataset/ontology semantics. |
| Data quality rules | **Data Health** checks and pipeline validation | `pipeline-expression` + health surfaces | Null, uniqueness, range, freshness, row-count, and schema checks are visible as health checks, not hidden test code. |
| Lineage | **Data Lineage** from source to dataset to object type and app | `lineage-service` | A presenter can click from `Flight.risk_score` to the producing pipeline and source datasets. |
| Aviation ontology | **Ontology Manager** object types, link types, properties, interfaces/value types where useful | `ontology-definition-service` + `object-database-service` | Object types, link types, property metadata, primary keys, display names, and permissions match Foundry semantics. |
| Operational writes | **Action Types** with rules, validation, side effects, and permissions | `ontology-actions-service` | Actions create/modify objects or links transactionally and enforce role/parameter validation. |
| Decision audit | **Action Log** object types and edit history | `audit-compliance-service` + ontology projections | Every action submission is queryable as an action-log object with actor, timestamp, action type, target object, outcome, and produced edits. |
| Notifications/writeback | **Action side effects**: Notifications and Webhooks | `notification-alerting-service` + outbound webhooks | Notifications and webhooks are configured as action side effects; failures follow Foundry-like writeback vs side-effect semantics. |
| Operations dashboard | **Quiver** dashboard and/or Workshop dashboard over ontology object sets | `application-composition-service` + `apps/web` | Dashboard uses ontology-backed object sets, filters, maps, tables, and action buttons. |
| MRO workbench | **Workshop** module with Object Table, Button Group, filters, variables, and Map/Object widgets | `application-composition-service` | App builder can configure the module without writing bespoke React for every interaction. |
| Copilot | **AIP Chatbot** with Ontology context, Document context, Function-backed context, and tools | `agent-runtime-service` + `retrieval-context-service` + `llm-catalog-service` | Tools map to Foundry tool types: Action, Object query, Function, Update application variable, Command, and Request clarification. |
| Branch demo | **Global Branching** across datasets/transforms/Ontology/Workshop/actions | `dataset-versioning-service` + repository branching + branch-aware services | Branch can modify a pipeline/Ontology/app, preview effects, run actions on branch, and merge after review. |
| Governance | Foundry permissions, markings/policies, action permissions, and platform security controls | `identity-federation-service` + `authorization-policy-service` + `audit-compliance-service` | Users only see and execute what their role/policy allows; the AIP chatbot inherits the same security boundary. |
| Observability | Foundry Observability/Data Health run metrics, traces, logs, and alerts | observability stack | P95 latency, failed runs, health checks, and AIP/tool activity are visible in a Foundry-style operational surface. |

---

## Required changes to the existing PoC narrative

### Replace OpenFoundry-first language

| Current wording/pattern | Replace with |
|---|---|
| “Spin up `connector-management-service`” | “Create a Data Connection sync; OpenFoundry implements this via `connector-management-service`.” |
| “Call `/api/datasets/v1/.../branches`” | “Create a Global Branch / dataset branch; OpenFoundry exposes a compatible API internally.” |
| “Register MCP tools” | “Configure AIP Chatbot tools; internally these may be implemented as MCP-style tools.” |
| “Built-in approvals in workflow service” | “Use Foundry-compatible human review/staged changes/resource protection where publicly supported; otherwise mark as OpenFoundry emulation.” |
| “Ollama fallback is Foundry-like” | “Use Foundry registered/BYOM model abstraction; Ollama is only an OpenFoundry-local provider unless validated in Foundry.” |
| “OpenLineage sink” | “Expose Foundry-style Data Lineage; OpenLineage can be an implementation detail.” |
| “Cedar / ABAC / RBAC” | “Foundry-style permissions, markings/policies, action permissions, and purpose/role controls; Cedar is implementation detail only.” |

### Preserve engineering detail separately

The existing service table in [`02-arquitectura-y-servicios.md`](02-arquitectura-y-servicios.md) is still valuable, but it must be read as an **implementation mapping**, not as the Foundry-native user journey. When implementing code, every service should expose Foundry-compatible resources and workflows instead of inventing product concepts that diverge from Foundry.

---

## Gaps that must not be oversold

| Gap | Current PoC risk | Foundry-native handling |
|---|---|---|
| Dedicated OpenSky connector | Public docs do not confirm a native OpenSky connector | Implement as a custom Data Connection/external transform; do not claim out-of-the-box connector parity. |
| Iceberg REST/Lakekeeper control plane | Foundry datasets are the user-facing abstraction | Keep Iceberg-compatible storage internal; expose dataset transactions, schema, permissions, and builds. |
| MCP naming | Public AIP docs describe Chatbot tools, not MCP as the user-facing concept | Use AIP Chatbot tool types in docs and UI. |
| Workflow sagas | Public docs confirm Actions, side effects, notifications, webhooks, and AIP Logic; generic saga engine parity is not confirmed | Implement workflows as Action + Function/AIP Logic + Notification/Webhook chains, and label saga behavior as OpenFoundry emulation. |
| Approval inbox | Public docs confirm resource protection/branch review and action confirmation/human review patterns, but not a generic approval primitive identical to the PoC wording | Model approvals as staged actions, branch/resource review, or explicit `ApprovalRequest` ontology objects. |
| Ollama local fallback | BYOM/registered model patterns exist, but local Ollama parity is not publicly confirmed | Keep provider abstraction; label Ollama as OpenFoundry-local demo fallback. |
| Quiver inside branch | Workshop branching docs note that non-Workshop elements such as Quiver dashboards are not modifiable on a branch | Do not make branch demo depend on modifying embedded Quiver cards. |
| Auditing every read/view | Action Log covers action submissions; public docs do not prove every object view is logged as the PoC describes | Guarantee action/write audit; mark read audit as environment-dependent unless implemented explicitly. |

---

## Foundry-native demo flow

1. **Data Connection:** create or show syncs for OpenSky historical/live, NOAA, BTS, FAA, OurAirports, and synthetic MRO.
2. **Raw datasets:** show landed datasets, schemas, transactions, permissions, and source attribution.
3. **Pipeline Builder / Code Repositories:** show bronze → silver → gold pipelines, incremental/streaming runs, and Data Health checks.
4. **Data Lineage:** trace `Flight.risk_score` from source datasets through feature transforms and model inference.
5. **Ontology Manager:** show object types (`Flight`, `Aircraft`, `Airport`, `MaintenanceEvent`, `WeatherObservation`, `Part`, `Engineer`, `Airline`, `AircraftModel`) and link types.
6. **Action Types:** show action rules and permissions for inspection, assignment, acknowledgement, reroute, and part order.
7. **Workshop/Quiver:** show Operations Live and MRO Triage Workbench built from ontology object sets, tables, filters, maps, and buttons.
8. **AIP Chatbot:** ask the demo prompts using Object query, Action, Function, and retrieval context tools; execute writes only with confirmation.
9. **Action Log and Governance:** show the resulting action log object, edited objects, actor, policy decision, and notification side effects.
10. **Global Branching:** create/test a risk-model branch, preview changes in Workshop, run branch-safe actions, review, merge, or discard.
11. **Observability:** close with Data Health, pipeline run metrics, AIP/tool traces, and latency/volume KPIs.

---

## Code adaptation checklist for OpenFoundry

Use this checklist to adapt OpenFoundry to the PoC rather than bending the PoC around OpenFoundry internals.

### Product surface
- [ ] Add Foundry-native labels and resource types in the UI: Data Connection, Dataset, Pipeline, Object Type, Link Type, Action Type, Workshop Module, AIP Chatbot, Global Branch.
- [ ] Hide raw microservice names from the customer-facing demo path.
- [ ] Add deep links from UI resources to lineage, health, action log, and branch context.

### Data layer
- [ ] Make dataset writes transaction-first and branch-aware.
- [ ] Represent raw, silver, gold, and ontology materializations as datasets with schema, versions, health, and lineage.
- [ ] Implement connector metadata so sources look like Data Connection syncs.

### Pipeline layer
- [ ] Persist transform graphs and schedules in a Pipeline Builder-like model.
- [ ] Surface quality checks as Data Health-style checks with status, owner, freshness, schema, and failure reason.
- [ ] Emit lineage at field/dataset/object-type level where feasible.

### Ontology and actions
- [ ] Support Foundry-like object/link/action type metadata, display names, validation, and permissions.
- [ ] Support action rules for create/modify/delete object and link edits.
- [ ] Support side effects with separate writeback vs post-commit behavior.
- [ ] Materialize an action log object type per action type.

### Workshop/Quiver-like app layer
- [ ] Implement object-table, filter, button group, map, object-card, and AIP-chat widgets as reusable configurable widgets.
- [ ] Ensure inline edits and action buttons are action-type backed.
- [ ] Ensure branch preview works for Workshop resources where supported, and document Quiver-like branch limitations.

### AIP layer
- [ ] Rename user-facing “MCP tools” to AIP Chatbot tools.
- [ ] Implement tool categories matching Foundry docs: Action, Object query, Function, Update application variable, Command, Request clarification.
- [ ] Enforce ontology permissions inside every tool invocation.
- [ ] Require confirmation for write actions unless a configured policy allows automatic execution.

### Governance and observability
- [ ] Enforce roles/policies consistently for humans, apps, actions, and AIP tools.
- [ ] Capture action submissions in queryable action-log objects.
- [ ] Add Data Health/Observability-style monitors for pipeline runs, freshness, schema, AIP tool latency, and UI p95.

---

## Definition of done

The PoC can be called **Foundry-native equivalent** only when all of the following are true:

1. A Foundry user can map every demo step to a known Foundry application or concept.
2. Any non-publicly-confirmed capability is explicitly marked as OpenFoundry emulation.
3. The AIP copilot uses the same security boundary as the UI user.
4. Every operational write is performed through an Action Type and appears in an Action Log.
5. Every data transformation has visible lineage and health checks.
6. Branching is demonstrated through Global Branching-compatible semantics, including documented limitations.
7. The demo script never claims parity for OpenFoundry-specific internals that Palantir public documentation does not support.
