# Rule lifecycle

Rules should follow a managed lifecycle rather than behaving like opaque embedded logic.

## Lifecycle

1. define the rule
2. attach it to semantic context
3. simulate or test it
4. apply it in controlled workflows
5. observe runs and outcomes

## Repository signals

OpenFoundry already supports CRUD, simulation, apply, and rule-run listing through the ontology backend.
