# Overview and lifecycle

Object types are created, enriched, secured, queried, and then consumed by application surfaces.

## OpenFoundry current vs target

| Dimension | OpenFoundry current | OpenFoundry target |
| --- | --- | --- |
| Definition surface | CRUD-style backend routes in `ontology-service` | richer authoring UX with proposal, review, and branching |
| Schema model | object types plus attached properties and interfaces | fully managed semantic lifecycle with staged rollout |
| Runtime usage | consumed implicitly by objects, search, rules, and views | explicit platform-wide contract for apps, AI, maps, and workflows |

## Lifecycle stages

1. define an object type
2. add required and optional properties
3. attach interfaces and shared semantic building blocks
4. create or ingest objects of that type
5. expose the type to search, workflows, reports, and apps
6. evolve the type safely over time

## Repository signals

- `services/ontology-service/src/handlers/types.rs`
- `services/ontology-service/src/models/object_type.rs`
- `services/ontology-service/migrations/*`

## Why this matters

If object-type lifecycle is not explicit, applications drift toward source-specific schema coupling and the ontology stops acting like a platform contract.
