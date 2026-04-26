# Interfaces

Interfaces define reusable semantic contracts across multiple object types.

## What this subtree covers

- interface definitions
- interface implementation on object types
- reuse of semantic patterns across domains

## Repository signals

The ontology backend already exposes:

- interface CRUD
- interface property CRUD
- attachment and detachment of interfaces to object types

Those capabilities are visible in `services/ontology-service/src/handlers/interfaces.rs` and the route map in `services/ontology-service/src/main.rs`.

## Section map

- [Interface lifecycle](/ontology-building/interfaces/interface-lifecycle)
- [Create an interface](/ontology-building/interfaces/create-an-interface)
- [Implement an interface](/ontology-building/interfaces/implement-an-interface)
- [Interface metadata reference](/ontology-building/interfaces/interface-metadata-reference)
- [Shared semantics patterns](/ontology-building/interfaces/shared-semantics-patterns)
- [Interface anti-patterns](/ontology-building/interfaces/interface-anti-patterns)
- [Interface-driven case flow](/ontology-building/interfaces/interface-driven-case-flow)
