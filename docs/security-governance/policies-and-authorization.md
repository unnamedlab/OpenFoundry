# Policies and authorization

Authorization in OpenFoundry is broader than role checks alone.

## Repository signals

The `auth-service` route surface includes:

- policy CRUD endpoints
- policy evaluation endpoints
- role and permission administration
- restricted-view administration

The implementation entry points live in:

- `services/auth-service/src/handlers/policy_mgmt.rs`
- `services/auth-service/src/handlers/permission_mgmt.rs`
- `services/auth-service/src/handlers/role_mgmt.rs`

## Why this matters

Operational platforms usually need a layered model:

- role-based access for broad capability boundaries
- policy-based evaluation for fine-grained control
- attribute-aware decisions for sensitive data and object operations

The current repo already contains the primitives for that model.
