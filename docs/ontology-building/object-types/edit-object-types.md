# Edit object types

Object types evolve over time as domains mature, applications grow, and governance requirements become clearer.

## Current editable fields

`UpdateObjectTypeRequest` currently allows updates to:

- `display_name`
- `description`
- `primary_key_property`
- `icon`
- `color`

The stable `name` is not currently part of the update payload, which is a healthy signal that OpenFoundry is already treating it as a durable semantic identifier.

## Current update flow

```text
editor
  -> PATCH/PUT ontology type endpoint
  -> ontology-service updates mutable metadata
  -> updated_at changes
  -> downstream applications consume revised semantics
```

## Why this matters

Editing should be cheap for presentation and explanatory metadata, but more controlled for keys and relationships because those changes ripple into:

- object ingestion
- workflow assumptions
- search behavior
- application contracts
