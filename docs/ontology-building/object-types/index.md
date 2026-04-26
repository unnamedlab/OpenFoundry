# Object types

Object types are the canonical containers for operational entities in the OpenFoundry ontology.

## What this subtree covers

- how object types represent real-world entities
- how they connect to interfaces, shared properties, and links
- how they become queryable and application-facing records

## Repository signals

The current implementation signals are strongest in:

- `services/ontology-service/src/handlers/types.rs`
- `services/ontology-service/src/models/object_type.rs`
- `proto/ontology/object.proto`

## OpenFoundry direction

Object types should become the stable semantic contract used by:

- workflow inputs and outputs
- reports and dashboards
- geospatial and graph layers
- AI retrieval and semantic search

## Section map

- [Overview and lifecycle](/ontology-building/object-types/overview-and-lifecycle)
- [Modeling patterns](/ontology-building/object-types/modeling-patterns)
- [Create an object type](/ontology-building/object-types/create-an-object-type)
- [Edit object types](/ontology-building/object-types/edit-object-types)
- [Object type metadata reference](/ontology-building/object-types/object-type-metadata-reference)
- [Object type anti-patterns](/ontology-building/object-types/object-type-anti-patterns)
- [End-to-end object type flow](/ontology-building/object-types/end-to-end-flow)
