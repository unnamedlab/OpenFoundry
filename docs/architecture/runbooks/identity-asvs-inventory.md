# S3.1.a — OWASP ASVS Level 2 inventory: `identity-federation-service`

> Stream: S3 Auth/sessions a Cassandra · Tarea S3.1.a
> Owner: identity-federation-service maintainers
> Status: substrate complete; per-row remediation tracked in S3.1.b–j

This inventory is the entry point for every ASVS L2 control we owe
on the identity surface. Each row maps a control to (a) the current
gap, (b) the substrate that already exists in code, and (c) the
follow-up sub-task in `migration-plan-cassandra-foundry-parity.md`.

## V2 — Authentication

| ASVS | Control | Gap today | Substrate | Follow-up |
|------|---------|-----------|-----------|-----------|
| 2.1.1 | Password length ≥ 12, breach-list checked | Length OK; no HIBP | `domain::security::hash_token` | S3.1.j (pen-test) |
| 2.2.1 | Anti-automation on auth endpoints | Missing | [`hardening::rate_limit`](../../../services/identity-federation-service/src/hardening/rate_limit.rs) | S3.1.h |
| 2.2.3 | Account lockout / progressive delay | Partial | rate_limit `LimitConfig::LOGIN` | S3.1.h |
| 2.2.4 | Anti-replay on refresh tokens | Missing | [`hardening::refresh_family`](../../../services/identity-federation-service/src/hardening/refresh_family.rs) | S3.1.f |
| 2.5.5 | Refresh token rotation | Missing | same | S3.1.f |
| 2.7.x | MFA: TOTP + WebAuthn | TOTP only (`bergshamra`) | [`hardening::webauthn`](../../../services/identity-federation-service/src/hardening/webauthn.rs) | S3.1.d |
| 2.10.x | Service-account creds in vault | Partial | `domain::api_keys` | S3.1.b |

## V3 — Session management

| ASVS | Control | Gap | Substrate | Follow-up |
|------|---------|-----|-----------|-----------|
| 3.2.1 | Session token entropy ≥ 64 bits | OK | `domain::security::random_token` | — |
| 3.3.1 | Idle timeout 30 min | Postgres-only today | [`sessions_cassandra::USER_SESSION_TTL_SECS`](../../../services/identity-federation-service/src/sessions_cassandra.rs) | S3.2.a |
| 3.3.2 | Absolute timeout / re-auth | OK | jwt access TTL | — |
| 3.5.x | Concurrent session limit | Missing | adapter — `count_active_sessions` (S3.2.d follow-up) | S3.2.d |
| 3.7.1 | Step-up auth for sensitive actions | Missing | Cedar `mfa_age_secs` claim | S3.1.i |

## V6 — Stored cryptography

| ASVS | Control | Gap | Substrate | Follow-up |
|------|---------|-----|-----------|-----------|
| 6.2.1 | Approved algorithms only | OK (HS256 dev, EdDSA prod) | `auth-middleware::jwt` | — |
| 6.2.5 | Keys in HSM/KMS | **HS256 secret in env** | [`hardening::vault_signer`](../../../services/identity-federation-service/src/hardening/vault_signer.rs) | S3.1.b |
| 6.2.7 | Key rotation policy | Missing | [`hardening::vault_signer::RotationPolicy`](../../../services/identity-federation-service/src/hardening/vault_signer.rs) | S3.1.c |

## V7 — Errors & logging

| ASVS | Control | Gap | Substrate | Follow-up |
|------|---------|-----|-----------|-----------|
| 7.1.1 | All auth events logged | Partial | [`hardening::audit_topic`](../../../services/identity-federation-service/src/hardening/audit_topic.rs) | S3.1.g |
| 7.1.3 | Logs include correlation id | Partial | `AuditEnvelope.correlation_id` | S3.1.g |

## V14 — Configuration

| ASVS | Control | Gap | Substrate | Follow-up |
|------|---------|-----|-----------|-----------|
| 14.2.1 | Minimum 3rd-party dep set | OK | `Cargo.toml` deny.toml | — |

## Pre-cutover gates

Before flipping any handler off the legacy Postgres path, the
following must be in place:

1. Vault transit signing key provisioned (`transit/keys/of-jwks-active`).
2. JWKS rotation cronjob deployed with the active + grace key set
   served at `/.well-known/jwks.json`.
3. WebAuthn relying-party config (`OF_WEBAUTHN_RP_ID`,
   `OF_WEBAUTHN_RP_ORIGIN`) deployed.
4. SCIM provisioning bearer token in Vault.
5. Refresh-token family detection in shadow mode for ≥7 days
   (compare CQL view vs sqlx view).
6. Audit topic `audit.identity.v1` accepting messages.
7. Redis sentinel reachable; `LimitConfig::LOGIN` in shadow mode for
   ≥7 days.
8. Cedar policy bundle deployed (`policies/identity_admin.cedar`).
9. Pen-test (S3.1.j) signed off.

## Pen-test (S3.1.j) — handover

See [`identity-pen-test-runbook.md`](identity-pen-test-runbook.md).
