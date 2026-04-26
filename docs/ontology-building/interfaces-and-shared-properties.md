# Interfaces and shared properties

Interfaces and shared property types are how the ontology avoids repeating the same semantic patterns across many object types.

## Repository signals

OpenFoundry already exposes first-class support for both ideas in `ontology-service`:

- interface routes under `/api/v1/ontology/interfaces`
- type attachment routes under `/api/v1/ontology/types/{type_id}/interfaces/{interface_id}`
- shared property type routes under `/api/v1/ontology/shared-property-types`
- type attachment routes for shared property types under `/api/v1/ontology/types/{type_id}/shared-property-types/{shared_property_type_id}`

Those routes are wired in `services/ontology-service/src/main.rs`.

## Why this matters

As ontology scope grows, teams need reusable semantic building blocks for concepts like:

- address and location data
- review and approval metadata
- scheduling windows
- ownership and accountability markers
- common display or formatting rules

Interfaces are the behavioral and structural contract. Shared properties are the reuse mechanism for repeated property bundles.

## OpenFoundry direction

These capabilities should become the foundation for:

- composable object models
- cleaner migration paths between ontology versions
- more stable application contracts across teams
