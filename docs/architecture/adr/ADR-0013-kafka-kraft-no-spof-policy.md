# ADR-0013: Kafka KRaft no-SPOF policy and upgrade procedure

- **Status:** Accepted
- **Date:** 2026-04-30
- **Deciders:** OpenFoundry platform architecture group
- **Related work:**
  - `infra/k8s/platform/manifests/strimzi/kafka-cluster.yaml` (Strimzi `Kafka` + `KafkaNodePool` manifest)
  - `tools/kafka-lint/check_kraft.py` (manifest-time contract lint)
  - `infra/k8s/platform/observability/prometheus-rules/kafka.yaml` (runtime alerts)
  - `smoke/chaos/kill-active-kafka-controller.sh` and `smoke/chaos/run.sh` (chaos-suite enforcement)
  - `infra/runbooks/kafka.md` §2.1 and `infra/runbooks/upgrade-playbook.md` (operational procedure)
  - [ADR-0011](./ADR-0011-control-vs-data-bus-contract.md) (which scopes Kafka to data-plane firehoses only)
  - [ADR-0012](./ADR-0012-data-plane-slos.md) (which sets the latency/durability SLOs this policy must keep green during upgrades)

## Context

OpenFoundry's data plane runs Apache Kafka in **KRaft combined mode** — three pods that are simultaneously controllers and brokers, deployed by Strimzi from `infra/k8s/platform/manifests/strimzi/kafka-cluster.yaml`. The cluster is the only durable transport for the five data-plane topics (`cdc.<source>`, `dataset.changes`, `lineage.events`, `model.inferences`, `audit.events` — see `infra/runbooks/kafka.md` §1) and is referenced by ADR-0011 as the data-plane half of the bus split and by ADR-0012 as the source of the data-plane durability SLOs (`acks=all`, `min.insync.replicas=2`, `unclean.leader.election.enable=false`).

That topology gives us a strict, fragile invariant set:

1. **Durability invariant.** `default.replication.factor=3` AND `min.insync.replicas=2` AND `unclean.leader.election.enable=false`. If any of the three drift, an `acks=all` producer can either silently lose data (unclean elections) or the cluster can stop accepting writes after a single broker loss (RF or min-ISR change).
2. **Availability invariant for the data path.** Losing one of three brokers must remain a tolerated event for `acks=all` producers (writes succeed because the remaining two satisfy `min.insync.replicas=2`).
3. **Availability invariant for the control path inside Kafka.** The KRaft quorum must always have exactly one active controller. Zero means metadata writes (topic create, leader elections, ISR updates) are stalled. More than one would mean a split-brain that violates Raft assumptions.
4. **No-SPOF for the controller role.** Killing the *active* controller — not just any pod — must result in a different pod being elected leader within the quorum's normal failover window.

Up to this point, those invariants were guarded only by **convention** (the comments at the top of the manifest) and by the fact that the people who wrote the manifest happened to pick the right values. Three concrete failure modes were therefore reachable from a normal PR:

- A reviewer flips `min.insync.replicas` from `2` to `1` to "unblock" a flaky test → durability silently degraded.
- A new metric scrape drifts and `UnderMinIsrPartitionCount` goes non-zero in production → producers start failing `acks=all` writes with `NotEnoughReplicas`, and nobody is paged because no alert was wired.
- A future refactor splits the node pool into separate controller/broker pools (or adds a fourth controller) and accidentally creates a quorum that can't tolerate a single loss → only discovered the next time a pod restarts.

## Decision

We make the four invariants above a **mechanically enforced contract** with four independent layers, plus a written upgrade policy that makes the gates explicit before any change to the Kafka cluster reaches production.

### Layer A — Manifest-time lint

`tools/kafka-lint/check_kraft.py` parses `infra/k8s/platform/manifests/strimzi/kafka-cluster.yaml` and refuses (exit 1) any manifest where:

- KRaft is not enabled (`strimzi.io/kraft: enabled` annotation missing) or any ZooKeeper config remains.
- `default.replication.factor != 3`, `min.insync.replicas != 2`, `unclean.leader.election.enable != false`, or the offsets/transaction-state-log replication factors don't match.
- Rack awareness is not configured against `topology.kubernetes.io/zone`.

The lint is wired into the existing CI workflow via `.github/workflows/kafka-lint.yml` and runs on every PR that touches the Strimzi manifests. The check costs milliseconds; it is the cheapest layer and the one that catches the largest class of regressions (PR review).

### Layer B — Runtime alerts

`infra/k8s/platform/observability/prometheus-rules/kafka.yaml` carries two KRaft-contract alerts in addition to the pre-existing operational ones (broker down, under-replicated, ISR shrink, offline partitions, consumer lag):

- **`KafkaUnderMinIsrPartitions`** (`severity: critical`, `for: 2m`): fires when `sum(kafka_server_replicamanager_underminisrpartitioncount) > 0`. This is the *direct* signal that `acks=all` producers are failing writes (`NotEnoughReplicas`), not the same as `UnderReplicatedPartitions`.
- **`KafkaActiveControllerCountAbnormal`** (`severity: critical`, `for: 3m`): fires when `sum(kafka_controller_kafkacontroller_activecontrollercount) != 1`. Single rule covers `0` (no controller, metadata stalled) and `>1` (split-brain). The 3-minute window absorbs normal failover events, including a Strimzi controller-pool rolling restart.

Both rules are unit-tested with `promtool test rules` in `infra/k8s/platform/observability/prometheus-rules/tests/kafka_kraft_test.yaml` and validated on every PR by `.github/workflows/prometheus-rules.yml`.

### Layer C — Chaos test for the active controller

`smoke/chaos/kill-active-kafka-controller.sh` (registered in `smoke/chaos/run.sh`) explicitly:

1. Queries `bin/kafka-metadata-quorum.sh ... describe --status` to find the *current active controller*'s broker id.
2. Resolves the corresponding Strimzi pod name (`<cluster>-<pool>-<id>`) and deletes it.
3. Waits for the cluster CR to be `Ready`, *and* asserts that a different broker id has taken leadership of the quorum, *and* that `sum(ActiveControllerCount) == 1` again.
4. Runs the existing data-plane smoke scenarios (`p2..p6`) under that fault to confirm no observable regression in the critical paths.

The pre-existing `kill-one-kafka-broker.sh` deletes whatever pod the API returns first; that pod *might* be the active controller but is not guaranteed to be. The new script makes the no-SPOF guarantee for the controller role **measured**, not assumed.

The chaos suite runs nightly (`.github/workflows/chaos-smoke.yml`, cron `17 4 * * *`) and on `workflow_dispatch`. It is intentionally not in the PR critical path — its cost is too high — but failing the nightly run is a release-blocker for the next day's work.

### Layer D — Upgrade policy

Every change that bumps any of the following is treated as a **Kafka KRaft upgrade**:

- the Strimzi operator chart version,
- `spec.kafka.version` in `kafka-cluster.yaml`,
- `spec.kafka.metadataVersion`,
- `KafkaNodePool.spec.replicas` or `roles`,
- the JBOD storage class or volume layout.

Such PRs follow the gates documented in `infra/runbooks/upgrade-playbook.md` §"KRaft upgrade preflight" and `infra/runbooks/kafka.md` §2.1. The policy in summary:

1. **Pre-flight gates (all must be green at PR-merge time):**
   - `tools/kafka-lint/check_kraft.py` clean against the post-upgrade manifest (Layer A).
   - In production, the last 1h of `KafkaUnderMinIsrPartitions` and `KafkaActiveControllerCountAbnormal` clean (Layer B).
   - `kubectl exec ... kafka-topics.sh --describe --under-replicated-partitions` returns empty.
   - The most recent successful chaos-smoke run is **≤ 7 days old** (Layer C).
2. **Order of changes:**
   1. Strimzi operator first (CRDs + controller) and **only** the operator — no Kafka version change in the same PR.
   2. Then `spec.kafka.version` bump, **one minor version at a time** per the upstream Strimzi upgrade matrix.
   3. Only after the cluster has been stable on the new `kafka.version` for at least one full chaos-smoke cycle (a nightly run since the version bump landed): bump `spec.kafka.metadataVersion`. The `metadataVersion` bump is **not reversible**; it is what locks the quorum into the new on-disk format.
3. **Abort criteria** (revert the PR or roll back via `helm rollback` / `kubectl apply` of the previous manifest):
   - Any of the two KRaft-contract alerts firing during the rollout.
   - The CR `Kafka/openfoundry` not reaching `Ready` within 30 minutes of `kubectl apply`.
   - Loss of quorum (sum of `ActiveControllerCount` stuck at `0` for more than 5 minutes).
4. **Forbidden in the same PR:** changing `min.insync.replicas`, `default.replication.factor`, `unclean.leader.election.enable`, or `KafkaNodePool.roles` together with a version bump. Each of those is its own PR, gated independently by Layer A.

## Consequences

- **Positive.** The four invariants are now defended at four independent layers (compile-time review, runtime alert, chaos test, written procedure). A single human mistake is not enough to break the data-plane durability contract; at least three layers have to fail in series.
- **Positive.** The chaos test converts the controller no-SPOF property from a design claim into a **measured nightly property**. Regressions are detected within 24 h, not at the next production incident.
- **Positive.** The upgrade policy makes ordering explicit (operator → kafka version → metadata version), which is the order Strimzi itself documents but which has never been written down inside the repo.
- **Neutral.** Any PR that touches the Strimzi manifest now triggers two CI jobs (`kafka-lint` + `prometheus-rules` if alerts also moved). Each is sub-second; the cost is negligible.
- **Negative.** The chaos test depends on `bin/kafka-metadata-quorum.sh` being present in the broker container image. That has been stable since Kafka 3.3 and is part of every Strimzi-shipped image we use; if Strimzi were ever to strip it, Layer C would need to fall back to parsing JMX. The cost of that future migration is small; we accept it.

## Notes

- This ADR does **not** change the Kafka cluster topology (still 3 pods, combined controller+broker mode). A future ADR may split the controller and broker roles into separate node pools — at which point Layer C will need a second chaos script targeting only the controller pool. The current script is written with that future split in mind: it parameterises `KAFKA_POOL` so a `controller`-only pool can be targeted without modification.
- Layers A–C are also referenced by `infra/runbooks/kafka.md` §2.1 (preflight checklist) so on-call engineers can find the same gates they need to run by hand during an emergency upgrade window.
