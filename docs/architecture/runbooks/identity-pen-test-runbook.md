# S3.1.j — Pen-test runbook: `identity-federation-service`

> Gate: must complete with **zero criticals** and **zero highs**
> before Stream S3 closes.

## Scope

Endpoints under `identity-federation-service`:

- `/login`, `/oauth/authorize`, `/oauth/token`, `/oauth/userinfo`
- `/.well-known/jwks.json`, `/.well-known/openid-configuration`
- `/mfa/totp/*`, `/mfa/webauthn/*`
- `/scim/v2/Users`, `/scim/v2/Groups`
- `/_admin/jwks/rotate`, `/_admin/scim/*`

Out of scope: downstream resource servers; those have their own
pen-test on `authorization-policy-service`.

## Threat model (ASVS L2 minimum)

1. **Credential stuffing** — automate `/login` with a 1M password
   list. Expect `LimitConfig::LOGIN` to deny after 10 reqs/60 s
   per (user, IP) (S3.1.h).
2. **Refresh-token theft** — replay a previously-issued refresh
   token after rotation. Expect family invalidation and audit event
   `RefreshTokenReplay` (S3.1.f / S3.1.g).
3. **JWKS confusion / kid spoofing** — present a JWT with a `kid`
   that maps to a retired key. Expect 401 and audit event.
4. **WebAuthn relying-party origin mismatch** — register from one
   origin, assert from another. Expect 400 (S3.1.d).
5. **SCIM auth bypass** — POST `/scim/v2/Users` from a human
   principal. Expect 403 (Cedar `forbid` rule, S3.1.i).
6. **Vault transit fail-closed** — block egress to Vault. Expect
   token issuance to fail (must NOT fall back to local HS256 key).
7. **Session fixation** — keep a session id across re-login. Expect
   the new login to issue a fresh session id and TTL the old row.
8. **OAuth state replay** — reuse the same `state` parameter twice.
   Expect 400 (TTL is 10 min; CQL row is consumed on use).

## Tooling

- ZAP automation framework (Docker image `zaproxy/zap-stable`).
- `pacu` (or in-house equivalent) for OAuth fuzzing.
- `webauthn.io` test relying-party for WebAuthn negative cases.
- `kafkacat` to subscribe to `audit.identity.v1` and assert that
  every test case produces the expected event (or none, depending
  on the case).

## Procedure

1. Spin up an isolated stack: `just compose-up identity-pentest`
   (uses `compose.yaml` with `identity-federation-service` +
   Cassandra + Postgres + Redis + Vault dev mode + Kafka in single-
   broker mode + a mock Apicurio).
2. Run ZAP automation pack `infra/scripts/zap-identity-pack.yaml`.
3. Run the targeted scripts for threats 1–8 above; collect findings.
4. File anything ≥ medium in JIRA; criticals/highs block the stream.
5. Sign off in this runbook below.

## Reporting

Findings live in `docs/architecture/security/findings/<date>.md`.
Closure of S3 requires **zero criticals + zero highs** signed off
by the security architect.

## Sign-off

- [ ] Security architect: ___
- [ ] Identity service maintainer: ___
- [ ] Date: ___
