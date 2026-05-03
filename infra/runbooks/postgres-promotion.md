# Runbook — Postgres replica promotion (region B)

> S7.4.b of the Cassandra/Foundry parity migration plan.
>
> Promotes one of the four region-B replica clusters
> (`pg-schemas-replica`, `pg-policy-replica`,
> `pg-runtime-config-replica`, `pg-lakekeeper-replica`) from standby
> to primary, breaking the streaming link with the (presumed dead)
> region-A primary.
>
> **This is a one-way operation.** Once a standby is promoted, the
> original region-A cluster cannot be re-attached as a follower
> without a fresh `pg_basebackup`. Plan rollback as a separate
> bootstrap exercise (see Step 7).

## When to promote

Trigger this runbook only when **all** of the following hold:

* Region A's primary `pg-<name>-rw` is unreachable from region B for
  > 5 minutes (validated by `kubectl --context region-a get cluster
  pg-<name>` returning `not found` or the cluster status
  `Unreachable`).
* The Cassandra application failover ([cassandra-app-failover.md](cassandra-app-failover.md))
  is already in progress — Postgres promotion is a **dependency** of
  the application cutover, not a precondition.
* The DR coordinator (on-call SRE + product owner) has authorised the
  cutover and the post-mortem channel is open.

Do **not** promote on partial degradation — the standby is read-only
serving across the region's reader services already, and a needless
promotion forces a mandatory bootstrap exercise to fail back.

## Pre-flight (60 s)

```sh
# Standby caught up?
kubectl --context region-b -n openfoundry exec pg-schemas-replica-1 -c postgres -- \
    psql -tAc "SELECT pg_is_in_recovery(), now() - pg_last_xact_replay_timestamp() AS lag;"
# Expected: t | <lag> with lag < 30s under healthy WAN conditions.

# No active large transactions on the standby?
kubectl --context region-b -n openfoundry exec pg-schemas-replica-1 -c postgres -- \
    psql -tAc "SELECT count(*) FROM pg_stat_activity WHERE state='active';"

# Backup chain healthy on the standby (so you have a recovery point
# straight after promotion).
kubectl --context region-b -n openfoundry get cluster pg-schemas-replica \
    -o jsonpath='{.status.lastSuccessfulBackup}{"\n"}'
```

## Step 1 — Confirm region A is gone

```sh
kubectl --context region-a -n openfoundry get cluster pg-schemas \
    || echo "region A unreachable (expected)"
```

If region A's API server responds and the primary is healthy, **stop**
— this runbook should not run.

## Step 2 — Promote the standby

CNPG promotes a replica cluster by setting `spec.replica.enabled:
false`. Once the operator notices the change, it stops the receiver
process, ends recovery, and the elected primary instance accepts
writes.

```sh
kubectl --context region-b -n openfoundry patch cluster pg-schemas-replica \
    --type merge \
    -p '{"spec":{"replica":{"enabled":false}}}'
```

Repeat for the other clusters that need to come online for the
failover scope (typically all four):

```sh
for c in pg-schemas-replica pg-policy-replica \
         pg-runtime-config-replica pg-lakekeeper-replica; do
    kubectl --context region-b -n openfoundry patch cluster "$c" \
        --type merge -p '{"spec":{"replica":{"enabled":false}}}'
done
```

## Step 3 — Verify promotion

```sh
kubectl --context region-b -n openfoundry exec pg-schemas-replica-1 -c postgres -- \
    psql -tAc "SELECT pg_is_in_recovery();"
# Expected: f

kubectl --context region-b -n openfoundry get cluster pg-schemas-replica \
    -o jsonpath='{.status.currentPrimary}{" "}{.status.phase}{"\n"}'
# Expected: pg-schemas-replica-1 Cluster in healthy state
```

The CNPG webhook re-renders `pg-<name>-replica-rw` so it now points at
the new primary.

## Step 4 — Switch application DSNs

Application services in region B are expected to consume the
`<bc>-db-dsn` Secret contract documented in
[`infra/k8s/helm/DATABASE_URL.md`](../k8s/helm/DATABASE_URL.md). The
region-B copy of each Secret should already point at the stable
`*-replica-rw` / `*-replica-ro` Services, so no per-chart Helm change
is needed in steady state.

If any service was hard-coded against `pg-<name>-rw.openfoundry.svc`
(region A name), patch its Deployment now:

```sh
kubectl --context region-b -n openfoundry set env deploy/<svc> \
    DATABASE_URL='postgresql://app@pg-schemas-replica-rw.openfoundry.svc:5432/app?sslmode=verify-full'
```

## Step 5 — Smoke test writes

```sh
kubectl --context region-b -n openfoundry exec pg-schemas-replica-1 -c postgres -- \
    psql -U app -d app -c "CREATE TABLE _dr_smoke (ts timestamptz default now()); \
                           INSERT INTO _dr_smoke DEFAULT VALUES; \
                           SELECT * FROM _dr_smoke; \
                           DROP TABLE _dr_smoke;"
```

Expected: insert succeeds, select returns one row, table drops cleanly.

## Step 6 — Pin the promotion in Git

Open a PR against `main` that:

1. Sets `replica.enabled: false` in the manifest at
   [`cnpg-replicas-region-b.yaml`](../k8s/platform/manifests/cnpg/region-b/cnpg-replicas-region-b.yaml)
   for the promoted clusters (so the next reconciliation does not
   silently re-enable replica mode).
2. Updates the backing `<bc>-db-dsn` Secret inputs (Vault / External
   Secrets / SealedSecrets) so `DATABASE_URL` and `DATABASE_READ_URL`
   resolve to the promoted `-replica-rw` / `-replica-ro` Services.
3. Adds the incident timestamp to the PR description for audit
   correlation.

## Step 7 — Failback procedure (after region A recovery)

Region A coming back online does NOT automatically re-attach. The
former primary is stale (it missed every write the new primary
accepted in B). To fail back:

1. **Decommission the old primary** in region A:
   ```sh
   kubectl --context region-a -n openfoundry delete cluster pg-schemas
   ```
2. **Bootstrap region A as a fresh replica** of the now-primary in B:
   create a new Cluster manifest in region A modelled on
   [`cnpg-replicas-region-b.yaml`](../k8s/platform/manifests/cnpg/region-b/cnpg-replicas-region-b.yaml)
   but with `externalClusters.host` pointing at
   `pg-<name>-replica-rw.openfoundry-region-b.svc.openfoundry.example.com`
   and `replica.enabled: true`.
3. **Wait for the new replica to fully catch up** (`pg_is_in_recovery()=t`,
   replication lag < 30s, last base backup OK).
4. **Promote region A back** by repeating Steps 2–6 with the contexts
   swapped. There is no shortcut — physical replication is one-way.
5. **Reverse the application DSN cutover** ([cassandra-app-failover.md](cassandra-app-failover.md)
   Step 6, mutatis mutandis).
6. **Revert the Step 6 PR.**

The full A → B → A loop is exercised quarterly as part of the S7.5.b
game day.

## Non-goals

* This runbook does **not** promote Cassandra (always multi-master,
  see [`cassandra-app-failover.md`](cassandra-app-failover.md)) or
  Lakekeeper region-B serving mode (transitions from RO to RW are
  documented in [`infra/k8s/platform/manifests/lakekeeper/region-b/README.md`](../k8s/platform/manifests/lakekeeper/region-b/README.md)
  and require coordination with the lakehouse on-call).
* This runbook does **not** rotate the `streaming_replica` user's
  credentials. That happens on the regular cert-manager schedule and
  is independent of failover.
