# Audit model

OpenFoundry’s audit model should explain not just that audit logs exist, but how audit is woven through platform capabilities.

## Repository signals

The current repo already includes:

- a dedicated `audit-service`
- a shared `libs/audit-trail` crate
- gateway middleware with audit concerns
- ontology actions and workflow paths that depend on traceability

## Why this matters

An audit model page is the right place to document:

- what gets recorded
- where audit events are emitted
- how operational teams investigate changes
- how audit supports governance and incident review

## OpenFoundry current vs target

| Dimension | OpenFoundry current | OpenFoundry target |
| --- | --- | --- |
| audit backend | dedicated service and shared crate | platform-wide consistent event taxonomy |
| integration points | gateway and semantic workflows already imply audit hooks | every critical object, action, workflow, and policy event recorded |
| investigation | service and workflow level | cross-capability traceability from UI to backend event trail |
