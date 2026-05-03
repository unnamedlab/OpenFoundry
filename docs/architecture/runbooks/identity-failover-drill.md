# S3.5 — Failover drill: identity stack on Cassandra

> Owner: identity-federation-service maintainers + SRE on-call.
> Gate: this drill must be executed and signed off before Stream S3
> closes, but it is **necessary and not sufficient** on its own; S3
> also requires the final gate `G-S3` in
> `docs/architecture/migration-plan-cassandra-foundry-parity.md` §18.

## Objectives

1. **Cassandra node failure.** Kill 1 of 3 Cassandra nodes during a
   sustained login storm. Validate that login and token-refresh P99
   stays **≤ 100 ms** for the full duration of the outage.
2. **`identity-federation-service` replica failure.** Kill 1 of N
   replicas of `identity-federation-service` during the same login
   storm. Validate **zero session loss** and that the load
   balancer drains the dead pod cleanly.
3. **JWKS rotation under Vault Transit.** Rotate the active Transit
   key version, publish both new and previous keys during the grace
   window, and verify that rollback restores signing to the previous
   key without dropping the new public key from JWKS.
4. **Identity hardening dependencies.** Verify Redis-backed rate
   limiting and `audit.identity.v1` publication stay healthy through
   identity pod failure and recover predictably from dependency
   outages.

## Pre-conditions

- Cassandra cluster running with `auth_runtime` keyspace at
  `NetworkTopologyStrategy {dc1: 3}` minimum (per
  [`infra/k8s/platform/manifests/cassandra/keyspaces-job.yaml`](../../../infra/k8s/platform/manifests/cassandra/keyspaces-job.yaml)).
- `identity-federation-service` deployed with **≥ 3 replicas** and
  PodDisruptionBudget `minAvailable: 2`.
- `session-governance-service` deployed with the revocation tables
  applied (S3.3).
- `oauth-integration-service` deployed with the pending-auth tables
  applied (S3.4).
- Locust/k6 rig configured against `/login` + `/oauth/token` —
  500 RPS sustained, 5 min duration.
- Vault Transit configured for identity signing:
  - `VAULT_ADDR` reachable from every `identity-federation-service`
    replica.
  - Either `VAULT_TOKEN` or Kubernetes role auth via `VAULT_ROLE`.
  - `VAULT_TRANSIT_KEY` set to the asymmetric signing key name.
  - `jwks_keys` table migrated in the identity control-plane
    database.
- Redis configured through `REDIS_URL` on every
  `identity-federation-service` replica. Set
  `IDENTITY_RATE_LIMIT_REDIS_REQUIRED=true` for this drill so a
  broken Redis DSN fails the rollout instead of silently falling back
  to in-memory counters.
- Kafka topic `audit.identity.v1` exists with production retention
  and the identity service has publish credentials. Set
  `IDENTITY_AUDIT_ENABLED=true` and
  `IDENTITY_AUDIT_FAILURE_POLICY=fail_closed` for the drill.
- Grafana dashboard *Identity SLO* loaded with these panels:
  - `histogram_quantile(0.99, rate(http_request_duration_seconds_bucket{service="identity-federation-service",route=~"/(login|oauth/token)"}[1m]))`
  - `sum(rate(http_requests_total{service="identity-federation-service",code=~"5.."}[1m]))`
  - `sum(rate(cassandra_client_request_timeouts_total{keyspace="auth_runtime"}[1m]))`
  - Active session count (CQL `SELECT count(*) FROM auth_runtime.user_session` sampled every 30 s).
  - Redis limiter errors: `sum(rate(redis_client_errors_total{service="identity-federation-service"}[1m]))`.
  - Audit publish attempts/failures from `/_admin/audit/metrics`.

## Procedure — Cassandra node kill

1. **Baseline (T-5 min):** start the load rig at 500 RPS. Confirm
   P99 ≤ 60 ms steady-state.
2. **Snapshot (T-1 min):** record active session count `N0`.
3. **Kill (T0):** `kubectl delete pod cassandra-1 -n cassandra` (no
   force, let the StatefulSet reschedule).
4. **Observe (T0 → T+5 min):**
   - Confirm CQL `consistency = LOCAL_QUORUM` reads/writes still
     succeed (2 of remaining 2 nodes form quorum).
   - Confirm Identity P99 ≤ **100 ms** for `/login` and
     `/oauth/token` throughout.
   - Confirm `cassandra_client_request_timeouts_total` per-second
     rate stays **= 0** (driver tolerates 1 dead node gracefully).
5. **Recovery (T+5 min):** wait until the rescheduled pod rejoins
   the ring (`nodetool status` → 3× `UN`). Confirm streaming/repair
   completes.
6. **Validate (T+10 min):** record active session count `N1`. Ratio
   `N1 / N0` must stay within 1 % (any drop = data loss / FAIL).
7. **Stop load.**

### Pass criteria

- P99 (`/login`) ≤ 100 ms for **every 1-min bucket** during the
  outage window.
- P99 (`/oauth/token`) ≤ 100 ms for **every 1-min bucket** during
  the outage window.
- Driver timeout rate = 0.
- Active session count drift < 1 %.
- Zero rows in Cassandra `system.batchlog_v2` left dangling after
  recovery.

## Procedure — `identity-federation-service` replica kill

1. **Baseline (T-5 min):** start the load rig at 500 RPS.
2. **Snapshot (T-1 min):** capture set of active `session_id`s
   sampled from the load rig (the rig holds them client-side).
3. **Kill (T0):** `kubectl delete pod identity-federation-service-<n>`
   (random replica, no force).
4. **Observe (T0 → T+2 min):**
   - Service mesh / k8s Service should drain the dead endpoint
     within 30 s; clients see at most a few `503` retries.
   - Remaining replicas absorb the traffic; P99 ≤ 100 ms restored.
5. **Validate (T+2 min):**
   - Replay each session id captured at T-1 min via `/oauth/userinfo`.
     **Every** session must still validate (no session loss).
   - Confirm `auth_runtime.user_session` count unchanged from T-1 min
     (no rows TTL'd because of the outage).
6. **Stop load.**

### Pass criteria

- Every pre-failure session id validates after the kill.
- 5xx error rate ≤ 0.1 % during the drain window.
- P99 recovers to ≤ 100 ms within 60 s of the pod removal.
- No row loss in `auth_runtime.user_session`.

## Procedure — JWKS rotation + rollback

1. **Baseline (T-5 min):** capture the currently published JWKS:
   `curl -fsS https://identity.openfoundry.local/.well-known/jwks.json`.
   Record:
   - active `kid`
   - active `public_pem` fingerprint
   - number of keys published
2. **Transit metadata check:** from an operator shell with Vault
   access, run `vault read transit/keys/$VAULT_TRANSIT_KEY` and
   record `latest_version`. The stable `kid` should match
   `<VAULT_TRANSIT_KEY>-v<latest_version>`.
3. **Rotate (T0):** call
   `POST /_admin/jwks/rotate` with an admin JWT that has
   `jwks:rotate` or `control_panel:write`.
4. **Validate publication (T0 → T+2 min):**
   - `/.well-known/jwks.json` returns **two** keys.
   - New key is `status = active` and has `kid =
     <VAULT_TRANSIT_KEY>-v<latest_version+1>`.
   - Previous key is `status = grace`.
   - Response includes `grace_until`; it must be 14 days after the
     rotation timestamp.
5. **Signing smoke:** issue a fresh access token and decode its JOSE
   header. The header `kid` must equal the new active `kid`; the
   signature must validate using the new JWKS entry.
6. **Rollback safety check:** call `POST /_admin/jwks/rollback`
   with either `{}` or `{ "target_kid": "<previous-kid>" }`.
7. **Validate rollback:**
   - Previous key is active again.
   - The demoted new key remains published as `status = grace`
     until its `grace_until` timestamp, so tokens minted during the
     failed rotation continue to validate.
   - A newly issued access token carries the restored previous `kid`.
8. **Re-rotate after remediation:** only after the root cause is
   fixed, call `POST /_admin/jwks/rotate` again and repeat the
   publication + signing smoke checks.

### Pass criteria

- Stable `kid` format is deterministic across pod restarts:
  `<VAULT_TRANSIT_KEY>-v<version>`.
- During normal rotation, JWKS publishes exactly one active key and
  the previous key in grace.
- During rollback, the restored key signs new tokens and the demoted
  key remains in grace; no token minted during the failed rotation is
  invalidated by publication.
- Vault Transit is the only signing path used for the active `kid`.

## Procedure — Redis limiter + identity audit

1. **Baseline limiter:** from a single source IP, send 11 invalid
   login attempts for the same email to
   `POST /api/v1/auth/login`. The 11th response must be `429` with a
   `Retry-After` header and JSON `retry_after_secs`.
2. **Cross-route isolation:** immediately send a refresh request to
   `POST /api/v1/auth/token/refresh` from the same IP. It may fail
   with `401` because the token is invalid, but it must not return
   `429`; route namespaces are independent.
3. **Audit smoke:** consume `audit.identity.v1` from the current
   offset and perform one failed login plus one successful login. The
   topic must receive two `IdentityAuditEvent::Login` envelopes with
   matching `correlation_id` when `x-correlation-id` is supplied.
4. **Redis failure mode:** block identity egress to Redis for one
   replica only. With `IDENTITY_RATE_LIMIT_REDIS_REQUIRED=true`, the
   replacement pod must fail readiness. Existing healthy replicas
   continue serving; do not accept a new pod that falls back to
   process-local counters during this drill.
5. **Audit failure mode:** block identity egress to Kafka for one
   request while `IDENTITY_AUDIT_FAILURE_POLICY=fail_closed`. Login
   must fail with `500`, and `/_admin/audit/metrics` must increment
   `failed` without incrementing `dropped`.
6. **Recovery:** restore Redis/Kafka egress and restart the affected
   pod. Repeat steps 1-3; all must pass.

### Pass criteria

- Login rate limit is enforced by Redis across pod restarts and
  returns `429` with `Retry-After`.
- `/api/v1/auth/login` and `/api/v1/auth/token/refresh` use separate
  limiter keys.
- `audit.identity.v1` receives expected login/session events with
  correlation headers.
- Fail-closed audit mode blocks token issuance while Kafka is
  unavailable.
- No identity pod becomes ready with Redis unavailable when
  `IDENTITY_RATE_LIMIT_REDIS_REQUIRED=true`.

## Reporting

Save Grafana snapshots, k6/Locust output, and `nodetool status`
before/after to `docs/architecture/security/drills/<date>/`. Also
save JWKS baseline/after/rollback JSON, Vault Transit metadata
redacted for secrets, and one decoded JWT header per phase. File any
failure in JIRA tagged `s3-failover-drill`.

## Sign-off

- [ ] SRE on-call: ___
- [ ] Identity service maintainer: ___
- [ ] Security architect (observer): ___
- [ ] Date: ___
- [ ] Drill outcome: PASS / FAIL — ___

> Failures in any pass-criteria block S3 closure. Re-run after
> remediation; even on PASS, S3 stays open until the rest of the §18
> closure gates are green and the sign-off above is complete.
