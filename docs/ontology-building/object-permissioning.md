# Object permissioning

Object permissioning is the point where ontology semantics and platform security meet.

## Repository signals

OpenFoundry already contains the primitives needed to build object-aware permissions:

- `auth-service` domain modules for RBAC, ABAC, and security
- restricted-view handlers and models
- policy evaluation endpoints
- ontology routes that already require JWT-backed protection

Relevant files include:

- `services/auth-service/src/domain/rbac.rs`
- `services/auth-service/src/domain/abac.rs`
- `services/auth-service/src/handlers/restricted_views.rs`
- `services/auth-service/src/handlers/policy_mgmt.rs`

## Why this matters

An operational ontology becomes dangerous if all users can see or mutate all objects equally.

Object permissioning is what makes the platform usable in:

- regulated environments
- multi-team operational scenarios
- sensitive incident and case management
- tenant-aware enterprise deployments

## OpenFoundry direction

The long-term design should support:

- object- and property-aware access decisions
- restricted or redacted views
- policy evaluation tied to user, group, and attribute context
- clear audit trails for every protected read or write path
