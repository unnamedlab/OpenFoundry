# ADR-0026: Retain and harden `identity-federation-service`

- **Status:** Accepted
- **Date:** 2026-05-02
- **Deciders:** OpenFoundry platform architecture group
- **Supersedes / supplements:**
  - The current implementation under
    [services/identity-federation-service/](../../../services/identity-federation-service/).
- **Related ADRs:**
  - [ADR-0020](./ADR-0020-cassandra-as-operational-store.md) —
    sessions and refresh-token state move to Cassandra with TTL.
  - [ADR-0024](./ADR-0024-postgres-consolidation.md) — declarative
    auth state (OIDC clients, JWKS metadata, SCIM mappings, role
    definitions) lives in `pg-schemas.auth_schema`.
  - [ADR-0027](./ADR-0027-cedar-policy-engine.md) — authorisation is
    Cedar; this ADR concerns authentication and identity only.

## Context

`identity-federation-service` is the OIDC provider, OAuth 2.1
authorisation server, refresh-token issuer and SCIM endpoint of the
platform. It is the single front door for human and service identity.

The audit in
[docs/architecture/audit-and-reference-no-spof.md](../audit-and-reference-no-spof.md)
records that the current implementation:

- Generates JWKS keys at startup and rotates them only on restart.
- Stores signing keys on a PVC, in plaintext.
- Has no MFA enforcement; password is the sole second factor.
- Has no SCIM 2.0 endpoint for upstream IdP provisioning.
- Has no refresh-token family / replay detection.
- Has limited audit (success/failure logs, no structured chain of
  custody for tokens or admin actions).
- Has no rate limiting on the token endpoint.
- Holds session state in Postgres, with all the consequences of
  [ADR-0020](./ADR-0020-cassandra-as-operational-store.md) for
  high-rate per-user state.

The choice is between **replacing** the service with an off-the-shelf
identity provider (Keycloak, Authentik, Zitadel, Ory Hydra/Kratos) and
**retaining and hardening** the existing custom service.

## Options considered

### Option A — Retain `identity-federation-service` and harden it (chosen)

Reasons that win the trade-off in our context:

- **Tight coupling to platform primitives** is a feature, not a bug
  here: the service issues tokens whose claims are consumed by the
  Cedar entity model in
  [ADR-0027](./ADR-0027-cedar-policy-engine.md). Owning the token
  shape means the entity model and the token contract evolve
  together without an integration adapter in the middle.
- **No dialect mismatch with the platform's data plane**: sessions
  and refresh tokens move to Cassandra with TTL
  ([ADR-0020](./ADR-0020-cassandra-as-operational-store.md)) using
  the same driver, the same observability and the same DR posture as
  the rest of the operational state.
- **Security-relevant code already passes review** (auth-middleware
  re-uses the same primitives and is exercised across every service);
  rewriting the surface is a larger risk than hardening it.
- **Operational surface is one Rust binary**, not a JVM cluster + a
  Postgres + an admin UI. Keycloak's operational footprint is
  defensible at scale we do not have.
- **Customisation cost is paid up-front and known**. Keycloak
  customisation is paid through SPIs, themes and forks, which
  drifts on every Keycloak upgrade.

What needs to change for this to be defensible:

- **Automatic JWKS rotation** with overlap windows and JWKS
  publication via `/.well-known/jwks.json`. Scheduled by Temporal
  ([ADR-0021](./ADR-0021-temporal-on-cassandra-go-workers.md)).
- **Key custody in HashiCorp Vault** via the **transit secrets
  engine**: signing operations happen inside Vault, the service
  never sees the private key bytes. Rotation is a Vault operation;
  the service holds only a reference (`key_name` + `key_version`).
- **MFA enforcement**: TOTP (mandatory for admin roles) and WebAuthn
  (passkeys + roaming authenticators) via `webauthn-rs`. Step-up
  required for any session that holds an `admin` or `marking-write`
  scope.
- **SCIM 2.0 endpoint** for upstream IdP provisioning (Okta, Entra,
  Google) so users / groups / role assignments can be pushed into
  the platform without bespoke connectors.
- **Refresh-token families with replay detection** (RFC 6749
  §10.4 + draft-ietf-oauth-security-topics §4.13): each refresh
  belongs to a family; reusing a previously rotated refresh
  invalidates the entire family and emits a `security.refresh.replay`
  event.
- **Rate limiting** at the token, authorise and userinfo endpoints,
  enforced at the edge gateway and re-enforced in the service.
- **Structured audit trail** for every admin action and every token
  issuance/refresh/revocation, written to the `audit-trail-service`
  pipeline (events end up in Iceberg via
  [ADR-0022](./ADR-0022-transactional-outbox-postgres-debezium.md)).
- **Annual third-party penetration test** scoped to the
  authentication and federation surface, with findings tracked as
  release blockers per finding severity.

### Option B — Replace with Keycloak (rejected)

- Off-the-shelf OIDC + SAML + OAuth + UMA + SCIM, mature, large
  community, Apache-2.0.
- Deal-breakers in our context:
  - **Operational complexity**: Keycloak HA on Kubernetes requires
    Infinispan replication, JGroups configuration, sticky sessions,
    a clustered cache and a Postgres backend tuned for the
    Keycloak schema. The operational footprint is closer to
    Temporal (a multi-component cluster with its own learning
    curve) than to a Rust binary.
  - **Customisation drift**: every claim shape, every protocol mapper
    and every credential type that does not match Keycloak's model
    is a Java SPI we own forever, with rebuild + redeploy on every
    Keycloak upgrade. Token shape coupling to Cedar
    ([ADR-0027](./ADR-0027-cedar-policy-engine.md)) becomes a
    cross-language contract.
  - **Operational dialect**: a JVM service is foreign to the
    platform's operational surface. Heap tuning, GC pauses,
    Infinispan cache sizing and Keycloak-specific dashboards are a
    new domain to own.
  - **Session storage**: Keycloak's session model is not
    pluggable to Cassandra without writing a Java SPI. We would
    either accept Keycloak's Infinispan model (more state to
    operate) or re-implement [ADR-0020](./ADR-0020-cassandra-as-operational-store.md)
    inside Java.
- The right time to consider Keycloak is if the team needs to
  support **federation protocols we do not implement** (SAML, UMA,
  WS-Federation) at a scale that justifies the operational cost.
  We do not, and OIDC + OAuth 2.1 + SCIM cover the foreseeable
  requirements.

### Option C — Replace with Zitadel / Authentik / Ory stack (rejected)

- Each is a credible point on the build-vs-buy curve. Each carries
  a different combination of (a) operational surface, (b) Rust /
  non-Rust dialect, (c) extension model. None gives us a meaningful
  enough advantage over hardened in-house to justify a rewrite of a
  surface that already exists, is already in use across every
  service via `auth-middleware`, and is on the critical path for
  every request.

### Option D — Status quo (rejected)

- Documented above; not defensible for production.

## Decision

We adopt **Option A**: **`identity-federation-service` is retained**
and hardened along the axes listed below. **Keycloak (and the other
off-the-shelf options) is explicitly rejected** for the reasons
documented in Option B and C. The service's session and refresh-token
state moves to Cassandra
([ADR-0020](./ADR-0020-cassandra-as-operational-store.md)).

## Hardening surface

### Key custody

- Signing keys (`RS256`/`ES256`) live inside Vault's **transit**
  engine. The service holds the key name; signing is a Vault API
  call.
- Encryption-at-rest keys for any locally cached material are also
  Vault-derived (transit `derive` mode).
- Vault is run with auto-unseal, audit logging to the platform
  audit pipeline, and HA per its own ADR (out of scope here, but
  recorded in the platform's secrets management runbook).

### JWKS rotation

- A Temporal Schedule
  ([ADR-0021](./ADR-0021-temporal-on-cassandra-go-workers.md))
  rotates the active signing key every 24 hours, keeping the
  previous key advertised in `jwks.json` for at least the longest
  refresh-token lifetime + clock-skew margin.
- `/.well-known/jwks.json` is generated from `pg-schemas.auth_schema`
  and is the single authoritative source for verifiers (every other
  service and `auth-middleware`).
- A separate Schedule revokes keys past the overlap window.
- Emergency rotation is a `tctl workflow start` away.

### Sessions and refresh tokens

- Stored in Cassandra keyspaces per
  [ADR-0020](./ADR-0020-cassandra-as-operational-store.md):
  `auth_runtime.sessions_by_token` and
  `auth_runtime.refresh_token_families`.
- TTL = absolute session lifetime + grace.
- Refresh-token families: each refresh belongs to a `family_id`. A
  rotation produces a new refresh with the same `family_id` and
  invalidates the prior. Re-presenting a rotated refresh:
  - Returns `invalid_grant`.
  - Emits `security.refresh.replay` to the audit pipeline.
  - Invalidates the entire family (cascade delete by `family_id`).
- Step-up: any session with an `admin` or `marking-write` scope is
  re-prompted for MFA every N minutes (configurable per realm).

### MFA

- TOTP (RFC 6238) via `totp-rs`. Required for accounts in any
  admin role.
- WebAuthn via `webauthn-rs` (passkeys + roaming authenticators).
- Recovery codes are one-time, hashed at rest, and emit an audit
  event on use.

### SCIM 2.0

- `/scim/v2/Users`, `/scim/v2/Groups` per RFC 7643/7644.
- Push-based provisioning from upstream IdPs.
- Mapping from upstream attributes to platform roles is declarative
  in `pg-schemas.auth_schema.scim_mappings`.

### Rate limiting

- Per-IP and per-client at the edge gateway.
- Per-account on the token endpoint inside the service (sliding
  window in Cassandra).
- Returns `429 Too Many Requests` with `Retry-After`.

### Audit

- Every admin action, every token issue / refresh / revoke and every
  MFA event is written to the audit pipeline via
  `libs/outbox::enqueue` ([ADR-0022](./ADR-0022-transactional-outbox-postgres-debezium.md))
  inside the same Postgres transaction as the state change.
- Audit events flow through Kafka to `audit-trail-service` and end
  up in Iceberg.

### Annual penetration test

- External vendor, scoped to the authentication and federation
  surface (token endpoints, MFA flows, SCIM, JWKS rotation, refresh
  rotation, session fixation, replay, CSRF, OIDC discovery
  conformance).
- Findings are tracked as release blockers per severity per the
  platform's standard vulnerability triage policy.

## Operational consequences

- New Vault transit roles + policies for the service.
- New Temporal Schedule + Workflow for JWKS rotation.
- New SCIM endpoint + admin UI hooks.
- New WebAuthn registration / authentication flows in the platform UI.
- Cassandra keyspaces for sessions / refresh families per
  [ADR-0020](./ADR-0020-cassandra-as-operational-store.md).
- Annual pen-test budget line.
- New runbook `infra/runbooks/identity-federation.md` covering
  emergency key rotation, mass refresh-family invalidation, MFA
  reset, SCIM mapping changes.

## Consequences

### Positive

- Identity surface stays in Rust, in the same operational dialect as
  the rest of the platform.
- Token shape and Cedar entity model evolve together without a
  cross-language integration boundary.
- Sessions inherit Cassandra's multi-DC posture for free.
- Hardening removes every concrete deficit listed in the audit.

### Negative

- We carry the cost of authoring and maintaining the security-
  sensitive surface ourselves. Mitigated by the annual pen-test
  and by the focused scope (no SAML, no UMA, no WS-Federation).
- Vault becomes a hard dependency for token issuance. Mitigated by
  Vault's own HA posture and by a short cache of the public verifier
  material to keep verification working during a brief Vault
  unavailability (signing requires Vault; verification does not).

### Neutral

- The team chose simplicity-of-operation over feature breadth. If
  the platform's identity requirements expand to protocols we do
  not implement, this ADR is the right place to reopen the decision.

## Follow-ups

- Implement migration plan task **S3.1** (Endurecimiento de
  identity-federation-service) end-to-end.
- Provision Vault transit engine and roles.
- Add WebAuthn flows to the platform UI.
- Add SCIM 2.0 endpoint + mapping table.
- Add refresh-family schema in Cassandra and the replay-detection
  path.
- Wire the JWKS rotation Schedule.
- Schedule the first annual pen-test.
- Author `infra/runbooks/identity-federation.md`.
- This ADR being `Accepted` does **not** by itself close Stream S3;
  S3 closes only when the runtime integrations are live, the
  failover/pen-test runbooks are signed off, and gate `G-S3` in the
  migration plan is green.
