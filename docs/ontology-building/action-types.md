# Action types

Action types are the controlled mutation layer of the ontology.

## What belongs here

In a full ontology platform, this area usually includes:

- action definitions
- parameters
- permissions
- side effects such as notifications or webhooks
- inline edits
- action metrics and logs

## OpenFoundry mapping

The current repo suggests this capability can be distributed across:

- `services/ontology-service`
- `services/workflow-service`
- `services/notification-service`
- `services/audit-service`
- `proto/ontology/action.proto`
- `proto/workflow/*`

## Why it matters

Actions are where the ontology stops being descriptive only and becomes operational.
