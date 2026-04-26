# ABAC and CBAC model

OpenFoundry already shows signs of moving beyond RBAC-only authorization.

## Repository signals

`auth-service` contains explicit domain modules for:

- `rbac`
- `abac`
- broader security modeling

Its migration history also references markings, CBAC-style controls, and restricted views.

## Why this matters

For an ontology-driven and data-sensitive platform, role checks alone are usually not enough.

Attribute- and context-aware access control becomes important for:

- tenant-aware experiences
- sensitive operational data
- object- and property-level restrictions
- conditional action execution
