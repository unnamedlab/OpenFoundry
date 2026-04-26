# Property design flow

Property design should follow a repeatable path instead of ad hoc field creation.

## Recommended flow

1. identify whether the information is identity, state, metric, governance, or display data
2. decide whether it should be a property, a linked object, or an interface field
3. define requiredness and default behavior
4. decide whether it should be user-editable, system-derived, or rule-managed
5. verify how it will appear in apps, reports, and workflows

## Sequence

```text
semantic designer
  -> object type design
  -> property CRUD in ontology-service
  -> object creation and mutation flows
  -> query/search/application consumption
```

## Why this matters

Property mistakes are expensive because they leak into every downstream surface.
