# Create an interface

Interfaces are reusable semantic contracts that let multiple object types share behavior and structure.

## Current request shape

Based on `CreateInterfaceRequest`, OpenFoundry currently supports:

| Field | Required | Purpose |
| --- | --- | --- |
| `name` | yes | stable interface identifier |
| `display_name` | no | user-facing label |
| `description` | no | semantic purpose |

## Current creation flow

```text
designer
  -> POST /api/v1/ontology/interfaces
  -> ontology-service persists ontology_interfaces row
  -> owner_id comes from authenticated subject
  -> interface becomes available for interface-property authoring and type binding
```

## Why this matters

Interfaces let OpenFoundry express repeatable concepts like `reviewable`, `schedulable`, or `geo-locatable` without duplicating property design across many object types.
