# Service consolidation map — 95 service directories → 33 ownership boundaries

> Companion to [ADR-0030](adr/ADR-0030-service-consolidation-30-targets.md).
> Tracks per-service status of the consolidation work declared in
> Stream S8.1 of the Cassandra/Foundry parity migration plan.
>
> Audit date: 2026-05-03. The live repository has **95 directories** under
> `services/`. S8 is now measured as ownership/deployment consolidation, not
> as physical reduction of the source tree to 30 directories. The three retired
> stubs `health-check-service`, `tool-registry-service` and
> `widget-registry-service` are documented below the current map and must not
> appear in Helm or compose runtime surfaces.

## Status legend

| Symbol | Meaning |
| ------ | ------- |
| `keep` | Stays as a top-level ownership boundary in the current target. |
| `merge → X` | Pending: routes/storage/types still owned by the legacy crate; will be merged into `X`. |
| `merged → X` | Done: legacy crate removed; `X` is the runtime owner. |
| `delete` | Service domain is fully owned elsewhere; legacy crate scheduled for deletion. |
| `sink` | Kafka consumer / relay; counted separately from ownership boundaries. |

## Map

| Current crate | Target | Status | Notes |
| ------------- | ------ | ------ | ----- |
| `agent-runtime-service` | `agent-runtime-service` | keep | absorbs `tool-registry-service`, `conversation-state-service`, `prompt-workflow-service` |
| `ai-application-generation-service` | `ai-evaluation-service` | merge → `ai-evaluation-service` | both share evaluation rig |
| `ai-evaluation-service` | `ai-evaluation-service` | keep | also absorbs `mcp-orchestration-service` |
| `ai-sink` | `ai-sink` | sink | Kafka → ML inference store |
| `analytical-logic-service` | `sql-bi-gateway-service` | merge → `sql-bi-gateway-service` | reusable expressions live next to SQL gateway |
| `app-builder-service` | (legacy) | delete | already retired in earlier R-prompts; verify Cargo workspace removal |
| `application-composition-service` | `application-composition-service` | keep | absorbs `application-curation-service`, `widget-registry-service` (S8.1.b), `developer-console-service`, `custom-endpoints-service`, `managed-workspace-service` |
| `application-curation-service` | `application-composition-service` | merge → `application-composition-service` | |
| `approvals-service` | `workflow-automation-service` | merge → `workflow-automation-service` | both backed by Temporal |
| `audit-compliance-service` | `audit-compliance-service` | keep | absorbs `sds-service`, `retention-policy-service`, `lineage-deletion-service` |
| `audit-sink` | `audit-sink` | sink | Kafka → Iceberg |
| `authorization-policy-service` | `authorization-policy-service` | keep | absorbs `cipher-service`, `network-boundary-service`, `checkpoints-purpose-service`, `security-governance-service` |
| `automation-operations-service` | `workflow-automation-service` | merge → `workflow-automation-service` | |
| `cdc-metadata-service` | `ingestion-replication-service` | merge → `ingestion-replication-service` | |
| `checkpoints-purpose-service` | `authorization-policy-service` | merge → `authorization-policy-service` | |
| `cipher-service` | `authorization-policy-service` | merge → `authorization-policy-service` | shares same secret store |
| `code-repository-review-service` | `code-repository-review-service` | keep | absorbs `global-branch-service`, `code-security-scanning-service` |
| `code-security-scanning-service` | `code-repository-review-service` | merge → `code-repository-review-service` | |
| `compute-modules-control-plane-service` | `pipeline-build-service` | merge → `pipeline-build-service` | same orchestrator |
| `compute-modules-runtime-service` | `pipeline-build-service` | merge → `pipeline-build-service` | runtime is sidecar of build |
| `connector-management-service` | `connector-management-service` | keep | absorbs `virtual-table-service`, OAuth-data side of `oauth-integration-service` |
| `conversation-state-service` | `agent-runtime-service` | merge → `agent-runtime-service` | |
| `custom-endpoints-service` | `application-composition-service` | merge → `application-composition-service` | |
| `data-asset-catalog-service` | `dataset-versioning-service` | merge → `dataset-versioning-service` | metadata/discovery only during transition; no runtime writes to `dataset_versions`, `dataset_branches`, `dataset_transactions` |
| `dataset-quality-service` | `dataset-versioning-service` | merge → `dataset-versioning-service` | |
| `dataset-versioning-service` | `dataset-versioning-service` | keep | sole runtime owner of `dataset_versions`, `dataset_branches`, `dataset_transactions`; Iceberg owns snapshots/data state |
| `developer-console-service` | `application-composition-service` | merge → `application-composition-service` | |
| `document-intelligence-service` | `retrieval-context-service` | merge → `retrieval-context-service` | shares parser pipeline |
| `document-reporting-service` | `notebook-runtime-service` | merge → `notebook-runtime-service` | |
| `edge-gateway-service` | `edge-gateway-service` | keep | |
| `entity-resolution-service` | `entity-resolution-service` | keep | specialised matching |
| `event-streaming-service` | `ingestion-replication-service` | merge → `ingestion-replication-service` | |
| `execution-observability-service` | `telemetry-governance-service` | merge → `telemetry-governance-service` | |
| `federation-product-exchange-service` | `federation-product-exchange-service` | keep | absorbs `marketplace-service`, `marketplace-catalog-service`, `product-distribution-service` |
| `geospatial-intelligence-service` | `ontology-exploratory-analysis-service` | merge → `ontology-exploratory-analysis-service` | |
| `global-branch-service` | `code-repository-review-service` | merge → `code-repository-review-service` | |
| `identity-federation-service` | `identity-federation-service` | keep | absorbs `oauth-integration-service` (auth side), `session-governance-service` |
| `ingestion-replication-service` | `ingestion-replication-service` | keep | |
| `knowledge-index-service` | `retrieval-context-service` | merge → `retrieval-context-service` | |
| `lineage-deletion-service` | `audit-compliance-service` | merge → `audit-compliance-service` | |
| `lineage-service` | `lineage-service` | keep | absorbs `workflow-trace-service` |
| `llm-catalog-service` | `llm-catalog-service` | keep | |
| `managed-workspace-service` | `application-composition-service` | merge → `application-composition-service` | |
| `marketplace-catalog-service` | `federation-product-exchange-service` | merge → `federation-product-exchange-service` | |
| `marketplace-service` | `federation-product-exchange-service` | merge → `federation-product-exchange-service` | |
| `mcp-orchestration-service` | `ai-evaluation-service` | merge → `ai-evaluation-service` | |
| `media-sets-service` | `media-sets-service` | keep | Foundry media sets runtime; owns media set transactions, item metadata and presigned object-store access. |
| `ml-experiments-service` | `model-catalog-service` | merge → `model-catalog-service` | |
| `model-adapter-service` | `model-catalog-service` | merge → `model-catalog-service` | |
| `model-catalog-service` | `model-catalog-service` | keep | |
| `model-deployment-service` | `model-deployment-service` | keep | absorbs `model-serving-service`, `model-evaluation-service`, `model-inference-history-service` |
| `model-evaluation-service` | `model-deployment-service` | merge → `model-deployment-service` | |
| `model-inference-history-service` | `model-deployment-service` | merge → `model-deployment-service` | |
| `model-lifecycle-service` | `model-catalog-service` | merge → `model-catalog-service` | |
| `model-serving-service` | `model-deployment-service` | merge → `model-deployment-service` | |
| `monitoring-rules-service` | `telemetry-governance-service` | merge → `telemetry-governance-service` | |
| `network-boundary-service` | `authorization-policy-service` | merge → `authorization-policy-service` | |
| `nexus-service` | (legacy) | delete | retire after `tenancy-organizations-service` and `federation-product-exchange-service` confirmed |
| `notebook-runtime-service` | `notebook-runtime-service` | keep | absorbs `document-reporting-service`, `spreadsheet-computation-service` |
| `notification-alerting-service` | `notification-alerting-service` | keep | |
| `oauth-integration-service` | split → `identity-federation-service` (auth) + `connector-management-service` (data OAuth) | merge | |
| `object-database-service` | `object-database-service` | keep | |
| `ontology-actions-service` | `ontology-actions-service` | keep | absorbs `ontology-funnel-service`, `ontology-functions-service`, `ontology-security-service` |
| `ontology-definition-service` | `ontology-definition-service` | keep | |
| `ontology-exploratory-analysis-service` | `ontology-exploratory-analysis-service` | keep | absorbs `ontology-timeseries-analytics-service`, `time-series-data-service`, `geospatial-intelligence-service`, `scenario-simulation-service` |
| `ontology-functions-service` | `ontology-actions-service` | merge → `ontology-actions-service` | |
| `ontology-funnel-service` | `ontology-actions-service` | merge → `ontology-actions-service` | |
| `ontology-indexer` | `ontology-indexer` | sink | |
| `ontology-query-service` | `ontology-query-service` | keep | |
| `ontology-security-service` | `ontology-actions-service` | merge → `ontology-actions-service` | |
| `ontology-timeseries-analytics-service` | `ontology-exploratory-analysis-service` | merge → `ontology-exploratory-analysis-service` | |
| `pipeline-authoring-service` | `pipeline-build-service` | merge → `pipeline-build-service` | |
| `pipeline-build-service` | `pipeline-build-service` | keep | absorbs authoring, schedule, compute modules |
| `pipeline-schedule-service` | `pipeline-build-service` | merge → `pipeline-build-service` | |
| `product-distribution-service` | `federation-product-exchange-service` | merge → `federation-product-exchange-service` | |
| `prompt-workflow-service` | `agent-runtime-service` | merge → `agent-runtime-service` | |
| `report-service` | (legacy) | delete | already covered by `document-reporting-service` |
| `retention-policy-service` | `audit-compliance-service` | merge → `audit-compliance-service` | |
| `retrieval-context-service` | `retrieval-context-service` | keep | absorbs `knowledge-index-service`, `document-intelligence-service` |
| `scenario-simulation-service` | `ontology-exploratory-analysis-service` | merge → `ontology-exploratory-analysis-service` | |
| `sdk-generation-service` | `sdk-generation-service` | keep | |
| `sds-service` | `audit-compliance-service` | merge → `audit-compliance-service` | |
| `security-governance-service` | `authorization-policy-service` | merge → `authorization-policy-service` | |
| `session-governance-service` | `identity-federation-service` | merge → `identity-federation-service` | |
| `solution-design-service` | `solution-design-service` | keep | |
| `spreadsheet-computation-service` | `notebook-runtime-service` | merge → `notebook-runtime-service` | |
| `sql-bi-gateway-service` | `sql-bi-gateway-service` | keep | absorbs warehousing, tabular, analytical-logic |
| `sql-warehousing-service` | `sql-bi-gateway-service` | merge → `sql-bi-gateway-service` | |
| `tabular-analysis-service` | `sql-bi-gateway-service` | merge → `sql-bi-gateway-service` | |
| `telemetry-governance-service` | `telemetry-governance-service` | keep | absorbs monitoring rules, health checks, execution observability |
| `tenancy-organizations-service` | `tenancy-organizations-service` | keep | |
| `time-series-data-service` | `ontology-exploratory-analysis-service` | merge → `ontology-exploratory-analysis-service` | |
| `virtual-table-service` | `connector-management-service` | merge → `connector-management-service` | |
| `workflow-automation-service` | `workflow-automation-service` | keep | absorbs automation-operations, approvals |
| `workflow-trace-service` | `lineage-service` | merge → `lineage-service` | |

## Retired service directories

These services were present in older S8 planning artifacts but are no longer
directories under `services/` and must not be rendered by Helm or compose:

| Retired service | Runtime owner | Closure |
| --------------- | ------------- | ------- |
| `health-check-service` | `telemetry-governance-service` | S8.1.a / S8.6.c |
| `tool-registry-service` | `agent-runtime-service` | S8.1.b |
| `widget-registry-service` | `application-composition-service` | S8.1.b / S8.6.b |

## Summary by status

| Status | Count |
| ------ | ----- |
| keep / ownership boundary | 33 |
| merge → X (pending) | 56 |
| delete scheduled for active legacy dirs | 3 |
| sink | 3 |
| **Total current service directories** | **95** |
| **Retired service directories tracked for references** | **3** |
| **Current target metric** | **33 ownership boundaries + 3 sinks across 5 Helm releases** |

## Execution sequence

Each merge is its own PR. The recommended ordering minimises rebase
churn:

1. **Sinks-and-leaves first** — services with zero downstream
   consumers drain to their parents. The three S8.1 retired stubs listed
   above have already drained and must stay absent from runtime manifests.
2. **Same-keyspace clusters** next — merges where source and target
   already share a Cassandra keyspace or Postgres schema (no data
   migration), e.g. ontology-actions absorbing
   ontology-functions/funnel/security.
3. **Cross-store merges** last — those that require schema work
   (e.g. consolidating audit-compliance with sds + retention +
   lineage-deletion all in `pg-policy`).
4. **Legacy macroservices** stay last (R-prompts in
   [`prompts-migracion-hasta-85-microservicios.md`](../../prompts-migracion-hasta-85-microservicios.md)).
