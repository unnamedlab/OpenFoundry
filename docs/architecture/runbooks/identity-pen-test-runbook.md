# S3.1.j — Pen-test runbook: `identity-federation-service`

> Gate: must complete with **zero criticals** and **zero highs**
> before Stream S3 closes, but this sign-off alone does **not** close
> S3; the stream still requires gate `G-S3` in the migration plan.

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

## Verifiable Checks

Run these checks in addition to ZAP. Save command output and topic
payloads with the final report.

| Control | Command / action | Expected result |
|---|---|---|
| Redis rate limit | Send 11 `POST /api/v1/auth/login` requests with the same `email` and source IP. | First 10 requests are auth failures; request 11 returns `429` and `Retry-After`. |
| Route isolation | After saturating login, call `POST /api/v1/auth/token/refresh` with an invalid token. | Response is `401`, not `429`; limiter key includes route. |
| Audit publication | Subscribe with `kcat -b $KAFKA_BOOTSTRAP_SERVERS -t audit.identity.v1 -C -o end`, then perform failed login, successful login and refresh replay. | Topic receives `login` and `refresh_token_replay` envelopes with UUIDv7 `event_id` and propagated `correlation_id`. |
| Vault Transit signing | Remove local JWT signing secrets from the pod and issue a token with Vault reachable. | Token header `kid` matches `<VAULT_TRANSIT_KEY>-v<version>` and validates via `/.well-known/jwks.json`. |
| Vault fail-closed | Deny pod egress to `$VAULT_ADDR` and issue a fresh token. | Token issuance fails; no HS256/local fallback appears in logs or JWT header. |
| WebAuthn origin binding | Begin registration with `OF_WEBAUTHN_RP_ORIGIN=https://id.example.test`, finish assertion from a mismatched origin. | Finish endpoint returns 4xx and does not advance credential counter. |
| WebAuthn replay counter | Replay a previously accepted assertion with the same or lower authenticator counter. | Login finish returns 4xx and emits MFA failure telemetry. |
| SCIM service principal | POST `/scim/v2/Users` as a service account with `scim_writer`. | User is created or idempotently returned when `externalId` already exists. |
| SCIM human principal deny | POST `/scim/v2/Users` with a human user JWT. | Request is denied by Cedar/authz path. |

## Reporting

Findings live in `docs/architecture/security/findings/<date>.md`.
Closure of S3 requires **zero criticals + zero highs** signed off
by the security architect **and** the rest of the final gates in
`migration-plan-cassandra-foundry-parity.md` §18.

## Sign-off

- [ ] Security architect: ___
- [ ] Identity service maintainer: ___
- [ ] Date: ___
