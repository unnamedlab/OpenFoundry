# Game day — Region A failure simulation

> S7.5.b of the Cassandra/Foundry parity migration plan.
>
> Scripted, time-boxed exercise that simulates a region-A outage and
> measures real RTO and RPO against the targets in
> [`dr-failover.md`](dr-failover.md). Run quarterly; record results in
> the changelog under `infra/runbooks/dr-results/`.

## Cadence and scope

* **Cadence**: quarterly. First two iterations in non-production
  staging; the third graduates to production with a 30-min
  pre-announced maintenance window.
* **Scope**: full stack. Cassandra, Postgres, Kafka, Lakekeeper, Ceph
  RGW, Temporal, application services.
* **Out of scope**: Ceph block storage regional DR (S8); end-user
  notifications (smoke recipients only, real users are pre-notified).

## Roles

| Role | Responsibility |
| ---- | -------------- |
| Incident Commander (IC) | Calls phases, declares RTO clock start/stop. |
| Scribe | Writes timeline to `dr-results/<date>.md` in real time. |
| Cassandra operator | Owns S7.2 procedures. |
| Postgres operator | Owns S7.4 procedures. |
| Streaming operator | Owns S7.3 procedures. |
| Storage operator | Owns S7.1 / Ceph multisite procedures. |
| Application validator | Drives Step 7 smoke. |

## Pre-game (T-24 h)

- [ ] Confirm region B replicas are in sync (lag < 60s) for all four
      Postgres clusters.
- [ ] Confirm MM2 lag p95 < 30s for the last 24h.
- [ ] Confirm `radosgw-admin sync status` = `caught up`.
- [ ] Snapshot baseline RPO/RTO measurements: produce a marker record
      to each major topic / table / Iceberg table named
      `dr_drill_baseline_<UTC>` and verify it lands in B.
- [ ] Pre-create the `dr-results/<UTC-date>.md` file from the template
      below.
- [ ] Notify users via the standard maintenance channel.

## Phase 1 — Inject the failure (T0)

The IC starts the RTO clock. Choose **one** simulation method:

### Option A — Network partition (preferred, most realistic)

```sh
# Drop all egress from region A's app namespace to region B and to
# the global edge. Reuses the chaos-mesh `NetworkChaos` CRD.
kubectl --context region-a apply -f infra/runbooks/dr-results/chaos/region-a-isolate.yaml
```

(Reference manifest in [`dr-results/chaos/region-a-isolate.yaml`](dr-results/chaos/region-a-isolate.yaml)
— add it as part of the first drill.)

### Option B — Forced API server shutdown (faster, less realistic)

```sh
# kops / kubeadm / managed-k8s specific. Document the exact command
# in dr-results/<date>.md.
```

### Option C — kill -9 every Pod in region A (chaotic but bounded)

```sh
kubectl --context region-a get pod -A -o name | xargs -P 32 -n 32 \
    kubectl --context region-a delete --grace-period=0 --force
```

The IC records `T0` UTC the moment the simulation is applied.

## Phase 2 — Detect and declare (target ≤ 5 min)

- [ ] Alertmanager fires `RegionAUnreachable` (PromQL:
      `up{region="a", job="kube-apiserver"} == 0` for > 60s).
- [ ] Pager paged the on-call IC.
- [ ] IC declares the DR event in the bridge channel; scribe records
      `T_detect`.

## Phase 3 — Execute failover (target ≤ 20 min)

Follow [`dr-failover.md`](dr-failover.md) steps 0–7. Scribe records the
timestamp at the start of each step.

| Step | Description | Target |
| ---- | ----------- | ------ |
| 1 | Postgres promote × 4 | ≤ 3 min |
| 2 | Cassandra `localDc=dc-b1` rolling restart | ≤ 5 min |
| 3 | Kafka `topicPrefix=dc-a.` rolling restart | ≤ 5 min |
| 4 | Lakekeeper RW + RGW master flip | ≤ 3 min |
| 5 | Temporal scale-up | ≤ 2 min |
| 6 | DNS / global LB cutover | ≤ 2 min |
| 7 | Smoke validation | ≤ 3 min |

The IC records `T_serving` when Step 7 returns first 200 OK.

## Phase 4 — Measure (T_serving + 5 min)

Compute the actual RTO and RPO:

```sh
# RTO
echo "RTO = T_serving - T0"

# RPO Postgres
kubectl --context region-b -n openfoundry exec pg-schemas-replica-1 -c postgres -- \
    psql -tAc "SELECT extract(epoch FROM (now() - max(updated_at))) AS rpo_s
               FROM <pick-a-high-traffic-table>;"

# RPO Kafka
kubectl --context region-b -n kafka exec -i deploy/kafka-tools -- \
    kafka-console-consumer.sh \
        --bootstrap-server openfoundry-b-kafka-bootstrap.kafka.svc:9092 \
        --topic dc-a.audit.events \
        --from-beginning --max-messages 1 --property print.timestamp=true \
        | tail -1
# Compare to the `dr_drill_baseline_<UTC>` marker timestamp.

# RPO Iceberg
spark-sql -e "SELECT max(commit_ts) FROM dc_a.smoke_replication.heartbeat_<baseline_ts>;"
```

Record all three in the results file.

## Phase 5 — Failback (T_serving + 30 min)

Failback is exercised even if production-realistic time is short. The
IC announces failback start; scribe records `T_failback_start`.

Follow [`dr-failover.md`](dr-failover.md) §9. Critical milestones:

- [ ] All four region-A CNPG clusters re-attached as replicas of B,
      lag < 60s.
- [ ] Cassandra repair across `dc1/dc2/dc3` complete.
- [ ] MM2 B → A reaches `caught up`.
- [ ] Ceph multisite reaches `caught up`.
- [ ] Reverse application cutover (DNS + Helm overlays).
- [ ] PR opened to revert the Phase-3 commit.

Record `T_failback_done`.

## Phase 6 — Post-mortem (within 48 h)

Author a post-mortem in `dr-results/<date>-postmortem.md` that
captures:

* Timeline (Phase 1 → Phase 5 with timestamps).
* Measured RTO / RPO vs targets.
* Deviations from the runbooks (what was unclear, what was
  obviously wrong).
* Action items, each linked to a tracking issue with an owner and
  deadline.

Update [`dr-failover.md`](dr-failover.md) and the per-component
runbooks with any corrections **before** closing the post-mortem.

## Results template

Save as `dr-results/<UTC-date>.md`:

```markdown
# DR drill — <YYYY-MM-DD>

| Field | Value |
| ----- | ----- |
| IC | @handle |
| Scribe | @handle |
| Failure mode | (Option A / B / C) |
| T0 (UTC) | |
| T_detect | |
| T_serving | |
| RTO (T_serving - T0) | |
| RPO Postgres | |
| RPO Kafka | |
| RPO Iceberg | |
| T_failback_done | |
| Targets met? | (yes / no — list misses) |

## Timeline
- T0+00:00 — Failure injected.
- T0+...   — ...

## Issues opened
- #NNN — ...
```

## Non-goals

* This drill does **not** simulate Ceph block storage regional loss
  (S8 territory). Postgres, Cassandra and Kafka stand on RBD locally
  in each region; an RBD CRR drill is a separate exercise.
* This drill does **not** validate user-facing notification fan-out
  to real recipients. Use a synthetic recipient pool.
