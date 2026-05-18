# Define ontologies

This page groups the ontology-definition concerns that, in a fuller platform, usually cover authoring, review, branching, shared semantics, and migration.

## Scope for OpenFoundry

The current repository already has the beginnings of this layer through:

- ontology domain contracts in `proto/ontology/*`
- the ontology service split (`ontology-definition-service` for schema/governance, `ontology-query-service` for reads, `ontology-actions-service` for mutations, `object-database-service` for storage) as the semantic backend
- smoke coverage for object types, interfaces, and type properties
- `identity-federation-service`, `authorization-policy-service`, and `audit-compliance-service` that can support reviewable change flows

## Planned subdomains

- ontology overview and lifecycle
- safe testing of ontology changes
- review and proposal workflows
- shared ontologies across teams
- migration between ontology versions
- usage and compute visibility for indexing and query workloads
