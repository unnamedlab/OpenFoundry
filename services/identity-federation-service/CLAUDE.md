# CLAUDE.md — services/identity-federation-service

> **SECURITY-CRITICAL.** Every change in this service touches the
> platform's authentication boundary. Default to additive changes;
> never weaken a check or remove a default without an explicit
> human-approved RFC.

## Surface area

| Subpackage | Owns |
|---|---|
| `handlers/` | HTTP endpoints (auth, RBAC, MFA, WebAuthn, SSO, JWKS) |
| `oidc/` | OIDC client + provider integration |
| `saml/` | SAML SP and IdP helpers |
| `webauthn/` | WebAuthn registration / login flows |
| `scim/` | SCIM 2.0 user/group provisioning |
| `cedarauthz/` | Cedar policy evaluation, `AdminGuard` middleware |
| `jwksrotation/` | Vault Transit signer, JWKS refresh |
| `sessionscassandra/` | Session store backed by Cassandra |
| `repo/` | Postgres repos (users, RBAC, restricted views) |
| `service/` | Domain services (login, register, refresh) |
| `server/` | chi router wiring |

## Boundaries you must not cross

- **Token signing** lives in `jwksrotation/vault_signer.go`. Do not
  inline-sign with `crypto/ecdsa` elsewhere; use the signer interface.
- **Password hashing** is centralized — search for `argon2`/`bcrypt`
  before adding any new path. Never reintroduce SHA-1/MD5.
- **`AdminGuard` middleware** (`cedarauthz/`) is the only sanctioned
  path to enforce admin-policy checks. Don't reimplement auth
  middleware in handlers.
- **Refresh-token rotation** must invalidate the previous token in the
  same DB transaction as it issues the new one. Look for the rotation
  helper in `service/` and reuse it.
- **MFA / WebAuthn** challenges must be single-use. Verify the
  challenge store deletion path before refactoring.

## Migrations

`internal/repo/migrations/` ships goose-style SQL. Once a migration is
released to any environment it is **immutable**; add a new file rather
than editing an existing one. Migrations that drop columns or relax
constraints require explicit human review (PII-touching schema).

## What an agent should do

- For pure handler/router shape: edit `handlers/` + add a test in the
  same package. Run `go test ./services/identity-federation-service/...`.
- For RBAC: changes usually span `handlers/rbac.go`, `repo/rbac.go`,
  and a Cedar policy in `policies/`. Touch all three.
- For SCIM: changes go through `scim/types.go` (wire) → `scim/pg_store.go`
  (storage) → handlers.

## Don't

- Don't bypass `cedarauthz` to "just do the check inline".
- Don't add a new password reset path without rate-limiting and audit
  emission via `libs/audit-trail`.
- Don't log token contents (access, refresh, MFA codes). The slog
  formatters in `libs/observability` redact known fields — keep the
  field names consistent.
- Don't read `docs/archive/INVENTORY-identity-federation-service.md`
  beyond surface scanning; it's a historical slice plan.

## Useful entry points

- `cmd/identity-federation-service/main.go` — boot sequence.
- `internal/server/server.go` — full route map.
- `policies/` — Cedar policies shipped with the service.
