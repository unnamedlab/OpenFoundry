# Object and link types

Object and link types are the structural heart of the ontology.

## What belongs here

This section covers the same broad concerns seen in mature ontology platforms:

- object type definitions
- property design
- shared properties
- structs and compositional types
- link definitions
- value types and reusable semantic primitives
- metadata and render semantics

## OpenFoundry mapping

The nearest current building blocks are:

- `proto/ontology/object.proto`
- `proto/ontology/ontology.proto`
- `services/ontology-service`
- `services/geospatial-service`

## Design direction

For OpenFoundry, object and link types should become the common contract used by:

- search and query surfaces
- maps and graph experiences
- actions and workflows
- AI retrieval and semantic context
