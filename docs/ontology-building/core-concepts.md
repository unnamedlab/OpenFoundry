# Core concepts

The core concepts of an operational ontology in OpenFoundry are straightforward even if implementation details evolve.

## Main building blocks

- object types: canonical representations of real-world entities
- properties: the fields that describe those entities
- link types: explicit relationships between entity types
- actions: controlled ways to modify or enrich operational state
- functions: code-backed logic attached to operational workflows
- interfaces: reusable semantic contracts across multiple types

## Repository signals

These concepts are already visible in the repo:

- `proto/ontology/object.proto`
- `proto/ontology/action.proto`
- `proto/ontology/ontology.proto`
- `services/ontology-service`
- P3 smoke scenarios for semantic and governance flows

## Design goal

The ontology should become the layer where data, permissions, automation, and application behavior align around the same business objects.
