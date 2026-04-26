# Policy evaluation flows

Policy evaluation is where abstract authorization models become operational decisions.

## Repository signals

`auth-service` already exposes explicit evaluation endpoints:

- `/api/v1/policies/evaluate`
- `/api/v2/admin/policies/evaluate`

Those endpoints are wired in `services/auth-service/src/main.rs` and backed by policy handlers in `services/auth-service/src/handlers/policy_mgmt.rs`.

## Why this matters

Documenting the evaluation flow helps explain:

- where decisions are made
- which user, role, group, and attribute inputs matter
- how restricted views and ontology operations can consume policy outcomes
