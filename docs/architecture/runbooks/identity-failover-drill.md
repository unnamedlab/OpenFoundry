# S3.5 — Failover drill: identity stack on Cassandra

> Owner: identity-federation-service maintainers + SRE on-call.
> Gate: this drill must be executed and signed off before Stream S3
> closes (see Definition of Done in
> `docs/architecture/migration-plan-cassandra-foundry-parity.md` §S3).

## Objectives

1. **Cassandra node failure.** Kill 1 of 3 Cassandra nodes during a
   sustained login storm. Validate that login and token-refresh P99
   stays **≤ 100 ms** for the full duration of the outage.
2. **`identity-federation-service` replica failure.** Kill 1 of N
   replicas of `identity-federation-service` during the same login
   storm. Validate **zero session loss** and that the load
   balancer drains the dead pod cleanly.

## Pre-conditions

- Cassandra cluster running with `auth_runtime` keyspace at
  `NetworkTopologyStrategy {dc1: 3}` minimum (per
  [`infra/k8s/cassandra/keyspaces-job.yaml`](../../../infra/k8s/cassandra/keyspaces-job.yaml)).
- `identity-federation-service` deployed with **≥ 3 replicas** and
  PodDisruptionBudget `minAvailable: 2`.
- `session-governance-service` deployed with the revocation tables
  applied (S3.3).
- `oauth-integration-service` deployed with the pending-auth tables
  applied (S3.4).
- Locust/k6 rig configured against `/login` + `/oauth/token` —
  500 RPS sustained, 5 min duration.
- Grafana dashboard *Identity SLO* loaded with these panels:
  - `histogram_quantile(0.99, rate(http_request_duration_seconds_bucket{service="identity-federation-service",route=~"/(login|oauth/token)"}[1m]))`
  - `sum(rate(http_requests_total{service="identity-federation-service",code=~"5.."}[1m]))`
  - `sum(rate(cassandra_client_request_timeouts_total{keyspace="auth_runtime"}[1m]))`
  - Active session count (CQL `SELECT count(*) FROM auth_runtime.user_session` sampled every 30 s).

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

## Reporting

Save Grafana snapshots, k6/Locust output, and `nodetool status`
before/after to `docs/architecture/security/drills/<date>/`. File
any failure in JIRA tagged `s3-failover-drill`.

## Sign-off

- [ ] SRE on-call: ___
- [ ] Identity service maintainer: ___
- [ ] Security architect (observer): ___
- [ ] Date: ___
- [ ] Drill outcome: PASS / FAIL — ___

> Failures of either pass-criteria block S3 closure. Re-run after
> remediation.
