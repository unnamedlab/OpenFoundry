# Audit model

> **Sensitive admin surface.** Audit data is itself sensitive and is
> gated by separate permissions. See the
> [Security overview](../security-overview.md), the
> [Shared responsibility model](../shared-responsibility-model.md), and
> the [Foundry public-docs parity policy](../../reference/foundry-public-docs-parity-policy.md).

OpenFoundry's audit model should explain not just that audit logs exist, but how audit is woven through platform capabilities.

## Repository signals

The current repo already includes:

- a dedicated `services/audit-compliance-service` (ledger + retention + lineage deletion)
- the `services/audit-sink` Kafka consumer that lands events into Iceberg for long-term storage
- a shared `libs/audit-trail` library every service uses to emit structured audit events
- gateway middleware with audit concerns (`libs/auth-middleware`)
- ontology actions and workflow paths that depend on traceability

## Why this matters

An audit model page is the right place to document:

- what gets recorded
- where audit events are emitted (via `libs/audit-trail` to Kafka topic `audit.events.v1`)
- how operational teams investigate changes (via the `/audit` UI route, backed by `audit-compliance-service`)
- how audit supports governance and incident review

## OpenFoundry current vs target

| Dimension | OpenFoundry current | OpenFoundry target |
| --- | --- | --- |
| audit backend | dedicated service (`audit-compliance-service`) + Iceberg sink (`audit-sink`) + shared library (`libs/audit-trail`) | platform-wide consistent event taxonomy |
| integration points | gateway and semantic workflows already imply audit hooks | every critical object, action, workflow, and policy event recorded |
| investigation | service and workflow level | cross-capability traceability from UI to backend event trail |

## Event model

As of `SG.16`, the hot audit ledger keeps both the existing
hash-chain fields and the normalized investigation fields modeled on
Foundry's `audit.3` schema. Each `audit_events` row has a stable row
`id`, an auditable `event_id` that can group related log lines, a unique
`log_entry_id`, optional `sequence_id`, actor identity, session or
service-account identifiers, product/service metadata, `categories`,
resource `entities`, request `origins`, `trace_id`, `outcome`, and
structured `error_metadata`, `request_fields`, and `result_fields`.

Service-initiated follow-up events can carry `parent_event_id` and the
same `trace_id` as the user-initiated request. UI and API filters can
therefore pivot by category, trace, event, subject, resource, and
classification without requiring service-specific action-name knowledge.

Audit data remains a sensitive resource. Reading audit events requires a
dedicated audit permission or auditor/admin role before per-event
classification, organization, and subject-scope filters run. The
`AUDIT_LOG_SECURITY_RETENTION` system policy marks audit-log resources
for long retention with legal-hold support.

## Delivery model

As of `SG.17`, the audit service exposes an `audit.3` delivery layer for
external SIEM polling and in-platform governed dataset exports.

- `audit_delivery_destinations` records per-organization SIEM API or
  OpenFoundry dataset destinations, validation status, schema version,
  and latest backfill status.
- `audit_delivery_files` records immutable delivery snapshots with
  time range, event count, duplicate `log_entry_id` count, checksum,
  byte size, content format, and generated content.
- `GET /api/v1/audit/delivery/files` is the date-range/schema listing
  surface. `GET /api/v1/audit/delivery/files/{id}/content` streams the
  NDJSON content or returns the content wrapper when JSON is requested.
- Destination setup, validation, and backfill are gated by dedicated
  audit delivery management permission; listing and retrieval remain
  behind the high-sensitivity audit-log read gate plus organization
  scope checks.

This mirrors the operational split documented for Foundry-style audit
monitoring: direct `audit.3` polling for SIEMs, or exported audit data
for analysis inside the governed platform.
