# Workflow composition

Workflow composition is where platform capabilities become repeatable operational processes.

## Repository signals

- `services/workflow-service`
- `services/notification-service`
- `services/ontology-service`
- `apps/web/src/routes/workflows`
- `proto/workflow/*`

## Why this matters

OpenFoundry is clearly aiming beyond static dashboards. Workflow composition is what allows the platform to coordinate:

- user actions
- system notifications
- rule outcomes
- approvals and escalations
- follow-up tasks and state transitions

## Design direction

This capability should eventually act as the orchestration layer that ties together ontology actions, notifications, analytics signals, and AI recommendations.
