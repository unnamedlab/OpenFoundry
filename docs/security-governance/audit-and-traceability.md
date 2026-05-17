# Audit and traceability

> **Sensitive admin surface.** Audit data is itself sensitive and is gated
> by separate permissions. Read the [Security overview](./security-overview.md)
> for how audit fits with the other control layers and the
> [Shared responsibility model](./shared-responsibility-model.md) for who
> delivers, retains, and reviews audit events. Anything modeled on a
> Foundry concept must follow the
> [Foundry public-docs parity policy](../reference/foundry-public-docs-parity-policy.md).

Auditability is a core platform feature, not an afterthought.

## Repository signals

OpenFoundry contains dedicated audit infrastructure through:

- `services/audit-compliance-service` — platform audit ledger, retention policies, lineage deletion subsystem
- `services/audit-sink` — Kafka → Iceberg consumer for the `audit.events.v1` stream (the long-term archive)
- `libs/audit-trail` — shared structured-audit-event library used by every service that needs to emit auditable records
- gateway audit middleware in `libs/auth-middleware` (records who accessed what, with which scope)
- ontology and action flows that call into audit-aware layers (`ontology-actions-service` records every action execution)

The service topology and CI smoke setup treat `audit-compliance-service` as a first-class runtime dependency.

## Why this matters

This is the layer that makes it possible to answer questions like:

- who changed an object
- which action was executed
- which policy allowed or blocked a decision
- what happened during a workflow or incident

For an operational platform, those answers are often required for trust, compliance, and post-incident learning.

## SG.16 ledger contract

`services/audit-compliance-service` stores audit rows in an append-only
hash chain while also promoting the fields needed for product-agnostic
security monitoring: action name, categories, event/log-entry IDs,
actor/session/service-account context, origins, trace ID, resource
entities, outcome, and structured error/request/result metadata.

The read side is intentionally gated separately from normal project
access. Callers need `audit-logs:view`, `audit:read`, `audit:view`, or an
auditor/admin role before the service applies event-level
classification, organization, and subject filters. Retention for the
audit-log resource class is seeded as the `AUDIT_LOG_SECURITY_RETENTION`
system policy.

## SG.17 delivery contract

Audit delivery is modeled as a separate sensitive admin surface. Security
operators can register per-organization destinations of type `siem_api`
or `openfoundry_dataset`, validate the setup, and backfill a date range
into immutable `audit.3` NDJSON delivery files.

`audit_delivery_destinations` stores the configured target, schema
version, validation state, backfill state, and setup metadata. Managed
operations require `audit-delivery:manage`, `audit-logs:manage`,
`audit:write`, or an auditor/admin role; read/list/content endpoints
still require the dedicated audit-log read gate.

`audit_delivery_files` stores generated delivery snapshots with
`start_time`, `end_time`, `event_count`, `duplicate_count`,
`content_sha256`, `content_bytes`, and raw content for retrieval through
`GET /api/v1/audit/delivery/files/{id}/content`. The list endpoint
supports organization, time-window, and schema-version filters so SIEMs
can poll by date range while the Audit UI can preview governed exports.

The current implementation validates configuration syntactically and
materializes local delivery files. External push scheduling and actual
dataset-write adapters are intentionally left to the next delivery
runtime layer; SG.17 provides the registry, access model, schema
contract, backfill bookkeeping, and retrieval API.
