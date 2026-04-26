# Audit and traceability

Auditability is a core platform feature, not an afterthought.

## Repository signals

OpenFoundry contains dedicated audit infrastructure through:

- `services/audit-service`
- `libs/audit-trail`
- gateway audit middleware
- ontology and action flows that call into audit-aware layers

The service topology and CI smoke setup also treat `audit-service` as a first-class runtime dependency.

## Why this matters

This is the layer that makes it possible to answer questions like:

- who changed an object
- which action was executed
- which policy allowed or blocked a decision
- what happened during a workflow or incident

For an operational platform, those answers are often required for trust, compliance, and post-incident learning.
