# Create an object type

Creating an object type is the first step in turning domain language into a reusable platform contract.

## Current request shape

Based on `CreateObjectTypeRequest` in `services/ontology-service/src/models/object_type.rs`, OpenFoundry currently supports:

| Field | Required | Purpose |
| --- | --- | --- |
| `name` | yes | stable identifier for the type |
| `display_name` | no | human-readable label |
| `description` | no | semantic and operational meaning |
| `primary_key_property` | no | identifies the object’s key field |
| `icon` | no | UI-facing visual hint |
| `color` | no | UI-facing semantic color |

## Current creation flow

```text
semantic designer
  -> POST /api/v1/ontology/types
  -> ontology-service persists object_types row
  -> owner_id is derived from JWT subject
  -> object type becomes available for property and interface attachment
```

## OpenFoundry current vs target

| Dimension | Current | Target |
| --- | --- | --- |
| creation | direct API creation | design-time workflow with preview and review |
| ownership | JWT subject stored as owner | richer project/team ownership model |
| UI hints | icon and color supported | fuller semantic presentation metadata |

## Design guidance

- choose a stable `name` that reflects domain language, not a source system table
- write `description` as operational intent, not technical implementation
- set `primary_key_property` only when the identifier model is clear
