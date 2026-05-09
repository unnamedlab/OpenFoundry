# CLAUDE.md — services/authorization-policy-service

> **SECURITY-CRITICAL.** This service is the platform's authorization
> decision point. A bug here is exploitable by every other service.
> Default to additive changes and add tests **before** changing
> evaluation logic.

## What it owns

- Cedar policy CRUD + evaluation (`handlers/cedar_policies.go`,
  `domain/`).
- ABAC evaluation (`domain/abac.go`).
- RBAC roles/groups/permissions (`handlers/rbac.go`,
  `repo/rbac.go`).
- Governance / project constraints (`handlers/security_governance.go`).
- Network-boundary policy (`handlers/network_boundary.go`).
- Cipher catalog and checkpoint/purpose records.

Restricted-view CRUD lives in `identity-federation-service`; *evaluation*
of restricted views happens here.

## Where to look first

| Concern | Open this |
|---|---|
| ABAC decision algorithm | `internal/domain/abac.go` (`Evaluate`) |
| Cedar HTTP surface | `internal/handlers/cedar_policies.go` |
| RBAC HTTP surface | `internal/handlers/rbac.go` |
| Persistent stores | `internal/repo/{rbac,security_governance,network_boundary}.go` |
| Wire models | `internal/models/models.go` |
| Router wiring | `internal/server/` |

## Invariants to preserve

- **`Evaluate` is the only ABAC decision function.** Don't fork ad-hoc
  evaluators in handlers. If you need a new attribute, add it to the
  request context and the policy schema, not as a side-channel.
- **Default deny.** Every new code path that returns an authorization
  result must default to deny on error/missing-data. There are tests
  asserting this — keep them green.
- **Decision log.** Every decision emits an audit event via
  `libs/audit-trail`. Don't add a fast-path that skips it.
- **Cedar entities** are typed by RID; never construct a Cedar entity
  with a raw string ID without going through the helper that validates
  the RID format.

## Migrations

`internal/repo/migrations/` is goose-managed. Schema changes here have
hot-path implications (every authorization check hits these tables).
Coordinate index changes with infra team; surface in PR description.

## Testing

```sh
go test ./services/authorization-policy-service/...
go test -tags integration ./services/authorization-policy-service/...
```

`internal/domain/abac_test.go` is the canonical place to add a new
"this scenario must {allow,deny}" assertion. **Add a test before
changing `Evaluate`.**

## Don't

- Don't add a "skip authz for service-X" fast path. Use a Cedar policy.
- Don't relax error handling to return "allow" on a transient DB error.
- Don't read `docs/archive/INVENTORY-authorization-policy-service.md`
  beyond surface scanning.
