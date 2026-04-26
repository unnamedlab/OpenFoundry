# Object sets and search

Object sets and search are the read model that turns ontology data into reusable operational context.

## Repository signals

The ontology backend already exposes:

- global ontology search through `/api/v1/ontology/search`
- graph access through `/api/v1/ontology/graph`
- object-set routes under `/api/v1/ontology/object-sets`
- object neighbors and object-view routes on individual objects

The relevant components are visible in:

- `services/ontology-service/src/handlers/search.rs`
- `services/ontology-service/src/handlers/object_sets.rs`
- `services/ontology-service/src/domain/object_sets.rs`

## Why this matters

Object sets are often the handoff format between capabilities:

- search results
- saved operational cohorts
- workflow targets
- analytics slices
- graph exploration pivots

In mature ontology platforms, saved object sets become shared resources used across applications. OpenFoundry is already pointing in that direction.

## Relationship to applications

This capability should feed:

- object exploration UIs
- rule and action execution scopes
- reports and dashboards
- map and graph experiences
