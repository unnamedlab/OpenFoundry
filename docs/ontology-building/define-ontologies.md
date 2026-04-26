# Define ontologies

This page groups the ontology-definition concerns that, in a fuller platform, usually cover authoring, review, branching, shared semantics, and migration.

## Scope for OpenFoundry

The current repository already has the beginnings of this layer through:

- ontology domain contracts in `proto/ontology/*`
- `ontology-service` as the semantic backend
- smoke coverage for object types, interfaces, and type properties
- auth and audit services that can support reviewable change flows

## Planned subdomains

- ontology overview and lifecycle
- safe testing of ontology changes
- review and proposal workflows
- shared ontologies across teams
- migration between ontology versions
- usage and compute visibility for indexing and query workloads
