# Kafka (Strimzi) Runbook

Date: April 29, 2026

OpenFoundry uses **Strimzi** (Apache-2.0) as the Kafka operator and **Apicurio
Registry** (Apache-2.0) as the Schema Registry. Confluent is explicitly
excluded (the Confluent Community License is not OSS).

Manifests: `infra/k8s/platform/manifests/strimzi/`
Related runbook: `infra/runbooks/ceph.md` (the brokers' JBOD volumes are
provisioned on the `ceph-rbd` `StorageClass` managed by Rook).

## 1. Deployed architecture

| Component              | Configuration                                                       |
|------------------------|---------------------------------------------------------------------|
| `Kafka` (KRaft)        | 3 combined brokers/controllers, no ZooKeeper                        |
| `KafkaNodePool` `kafka`| `roles: [controller, broker]`, JBOD 2 × 200Gi on `ceph-rbd`         |
| Durability             | `default.replication.factor=3`, `min.insync.replicas=2`, `unclean.leader.election.enable=false` |
| Listeners              | `plain` (9092 internal), `tls` (9093 internal, mTLS)                |
| Base topics            | `cdc.<source>`, `dataset.changes`, `lineage.events`, `model.inferences`, `audit.events` (P=12, RF=3) |
| Schema Registry        | Apicurio Registry, Postgres backend (CNPG `apicurio-pg`)            |
| NetworkPolicy          | `CiliumNetworkPolicy` per service (producer/consumer)               |

## 2. Routine operations

### 2.1 Rolling upgrade (operator and/or brokers)

Strimzi reconciles any change to the `Kafka` CR in a rolling fashion,
honoring `min.insync.replicas`. The standard procedure:

```bash
# 1. Update the operator (CRDs + controller).
helm repo update strimzi
helm upgrade -n kafka strimzi-operator strimzi/strimzi-kafka-operator \
  -f infra/k8s/platform/manifests/strimzi/values-strimzi-operator.yaml

# 2. Bump the Kafka version. Edit `spec.kafka.version` and
#    `spec.kafka.metadataVersion` in kafka-cluster.yaml following
#    https://strimzi.io/docs/operators/latest/deploying#con-upgrade-cluster-str
kubectl apply -f infra/k8s/platform/manifests/strimzi/kafka-cluster.yaml

# 3. Watch the rollout. Pods restart one at a time.
kubectl -n kafka get pods -l strimzi.io/cluster=openfoundry -w
kubectl -n kafka get kafka openfoundry -o jsonpath='{.status.conditions}'
```

Force a manual rolling restart (e.g. after rotating TLS):

```bash
kubectl -n kafka annotate pod -l strimzi.io/cluster=openfoundry,strimzi.io/kind=Kafka \
  strimzi.io/manual-rolling-update=true
```

Mandatory pre-flight before any upgrade (see also
[ADR-0013](../../docs/architecture/adr/ADR-0013-kafka-kraft-no-spof-policy.md)
§"Layer D — Upgrade policy" and `infra/runbooks/upgrade-playbook.md`
§"KRaft upgrade preflight"):

```bash
# 1. Lint the manifest against the KRaft contract (Layer A).
python3 tools/kafka-lint/check_kraft.py

# 2. No under-replicated partitions (runtime operation, Layer B).
kubectl -n kafka exec -it openfoundry-kafka-0 -c kafka -- \
  bin/kafka-topics.sh --bootstrap-server localhost:9092 --describe --under-replicated-partitions
# Expected output: empty.

# 3. Healthy KRaft quorum: exactly 1 active controller.
kubectl -n kafka exec -it openfoundry-kafka-0 -c kafka -- \
  bin/kafka-metadata-quorum.sh --bootstrap-server localhost:9092 describe --status

# 4. The two KRaft alerts from the Prometheus rules must be silent
#    for the last hour (Layer B):
#      - KafkaUnderMinIsrPartitions
#      - KafkaActiveControllerCountAbnormal
#    Verify in Alertmanager / the Grafana dashboard.

# 5. The last green run of the chaos-suite (Layer C) must be ≤ 7 days old.
#    Check this in the `Chaos Smoke (Data Plane no-SPOF)` workflow.
```

### 2.2 Cluster expansion (adding brokers)

With `KafkaNodePool` the operation is declarative: bump `spec.replicas`
and apply. Strimzi creates the new pods, formats their JBOD disks, and
adds them to the quorum. **Existing partitions are not moved automatically** —
they must be reassigned (section 2.3).

```bash
# 1. Edit `spec.replicas: 3 → 5` in KafkaNodePool `kafka`.
kubectl -n kafka edit kafkanodepool kafka

# 2. Wait for the new brokers to become Ready.
kubectl -n kafka get pods -l strimzi.io/pool-name=kafka -w

# 3. Verify they show up in the cluster.
kubectl -n kafka exec -it openfoundry-kafka-0 -c kafka -- \
  bin/kafka-broker-api-versions.sh --bootstrap-server localhost:9092 | grep id:
```

To shrink the cluster (scale-in), you must first **drain** the brokers to
be removed using reassignment (section 2.3), and only then lower `replicas`.
Strimzi rejects the scale-down if it detects partitions residing on the
target broker.

### 2.3 Partition reassignment

Strimzi exposes declarative reassignment via the `KafkaRebalance` CR (Cruise
Control). It is the **recommended** method: it optimizes for throughput, disk
space, and leadership simultaneously.

```bash
# 1. Enable Cruise Control on the Kafka CR (one-time).
kubectl -n kafka patch kafka openfoundry --type merge -p '
spec:
  cruiseControl: {}
'

# 2. Create a rebalance proposal.
cat <<'EOF' | kubectl apply -f -
apiVersion: kafka.strimzi.io/v1beta2
kind: KafkaRebalance
metadata:
  name: full-rebalance
  namespace: kafka
  labels:
    strimzi.io/cluster: openfoundry
spec:
  goals:
    - RackAwareGoal
    - ReplicaCapacityGoal
    - DiskCapacityGoal
    - NetworkInboundCapacityGoal
    - NetworkOutboundCapacityGoal
    - ReplicaDistributionGoal
    - LeaderReplicaDistributionGoal
EOF

# 3. Cruise Control evaluates and publishes `status.optimizationResult`.
kubectl -n kafka get kafkarebalance full-rebalance -o yaml

# 4. Approve the proposal to execute it.
kubectl -n kafka annotate kafkarebalance full-rebalance \
  strimzi.io/rebalance=approve
```

To drain **a specific broker** (e.g. broker 4 before a scale-in), use the
`remove-brokers` mode:

```yaml
apiVersion: kafka.strimzi.io/v1beta2
kind: KafkaRebalance
metadata:
  name: drain-broker-4
  namespace: kafka
  labels:
    strimzi.io/cluster: openfoundry
spec:
  mode: remove-brokers
  brokers: [4]
```

Manual mode (fallback without Cruise Control), using the scripts shipped in
the Kafka image:

```bash
kubectl -n kafka exec -it openfoundry-kafka-0 -c kafka -- bash
# inside the pod:
cat > /tmp/topics.json <<'EOF'
{ "topics": [ { "topic": "dataset.changes" } ], "version": 1 }
EOF
bin/kafka-reassign-partitions.sh --bootstrap-server localhost:9092 \
  --topics-to-move-json-file /tmp/topics.json \
  --broker-list "0,1,2,3,4" --generate
# review the plan, save it as reassignment.json, then:
bin/kafka-reassign-partitions.sh --bootstrap-server localhost:9092 \
  --reassignment-json-file /tmp/reassignment.json --execute
bin/kafka-reassign-partitions.sh --bootstrap-server localhost:9092 \
  --reassignment-json-file /tmp/reassignment.json --verify
```

## 3. Base topics

The five data-plane topics are created via `KafkaTopic` CRs
(`infra/k8s/platform/manifests/strimzi/kafka-topics.yaml`). To add a new CDC topic:

```bash
cat <<'EOF' | kubectl apply -f -
apiVersion: kafka.strimzi.io/v1beta2
kind: KafkaTopic
metadata:
  name: cdc.mysql                 # cdc.<source>
  namespace: kafka
  labels:
    strimzi.io/cluster: openfoundry
    openfoundry.io/topic-class: cdc
spec:
  partitions: 12
  replicas: 3
  config:
    min.insync.replicas: "2"
    cleanup.policy: compact
    retention.ms: "604800000"
EOF
```

Never lower `partitions` (Kafka does not support it). To raise the
partition count, edit the CR; the operator propagates the change. To change
RF, use reassignment (section 2.3).

## 4. Apicurio Registry

* In-cluster endpoint: `http://apicurio-registry.apicurio.svc:8080`
* Backend: PostgreSQL HA via CNPG (`apicurio-pg-rw.apicurio.svc:5432`).
* Backups: inherit the CNPG policy from the cluster (Barman/WAL archive to
  Ceph S3).

Typical operations:

```bash
# List schema groups.
curl -s http://apicurio-registry.apicurio.svc:8080/apis/registry/v3/groups | jq

# Upload an Avro schema.
curl -X POST -H 'Content-Type: application/json; artifactType=AVRO' \
  --data-binary @schemas/dataset-changes.avsc \
  http://apicurio-registry.apicurio.svc:8080/apis/registry/v3/groups/openfoundry/artifacts
```

Postgres backend recovery: see `infra/runbooks/disaster-recovery.md`
(CNPG section); the Apicurio `Cluster` follows the same procedure as
any other `postgresql.cnpg.io/v1 Cluster` on the platform (reference
template at `infra/k8s/platform/manifests/cnpg/templates/cluster.yaml`; the former
Apache Polaris subchart was retired by
[ADR-0008](../../docs/architecture/adr/ADR-0008-iceberg-rest-catalog-lakekeeper.md)).

## 5. NetworkPolicies

The `CiliumNetworkPolicy` resources in `network-policies.yaml` apply
**default-deny** to the Kafka and Apicurio pods, and open ports only to
pods labeled with:

* `openfoundry.io/kafka-role: producer | consumer | producer-consumer`
* `openfoundry.io/service: <name>` (for example, `dataset-versioning-service`, `lineage-service`…)

When a new service is onboarded:

1. Label the Deployment/StatefulSet with both labels.
2. Add the name to the corresponding `matchExpressions[*].values` list in
   `network-policies.yaml`.
3. Create a `KafkaUser` with the minimum ACLs (in a separate PR).
4. `kubectl apply -f infra/k8s/platform/manifests/strimzi/network-policies.yaml`.

## 6. Troubleshooting

| Symptom                                 | Likely cause                                | Action                                                 |
|-----------------------------------------|---------------------------------------------|--------------------------------------------------------|
| Producer receives `NOT_ENOUGH_REPLICAS` | `min.insync.replicas=2` and a broker is down | Verify `kubectl get pods -n kafka`, wait for the rolling. Canonical alert: `KafkaUnderMinIsrPartitions` (see [ADR-0013](../../docs/architecture/adr/ADR-0013-kafka-kraft-no-spof-policy.md) §"Layer B"). |
| `LEADER_NOT_AVAILABLE` after scale-in   | Partitions remained on a removed broker     | Reassign (section 2.3) and retry                       |
| Pods Pending due to PVC                 | `ceph-rbd` out of capacity                  | See `infra/runbooks/ceph.md` (OSD expansion)           |
| Apicurio returns 503                    | CNPG primary in failover                    | `kubectl -n apicurio get cluster apicurio-pg`          |
| NetworkPolicy blocks legitimate traffic | Service labeling is missing                 | See section 5 and reapply policies                     |
| Suspected loss of active controller (zero) or split-brain (two) | Operator bug, network failure between controllers, or inconsistent state after an upgrade | `KafkaActiveControllerCountAbnormal` alert. Inspect `bin/kafka-metadata-quorum.sh ... describe --status`. The nightly validation covering this case is `smoke/chaos/kill-active-kafka-controller.sh` (see [ADR-0013](../../docs/architecture/adr/ADR-0013-kafka-kraft-no-spof-policy.md) §"Layer C"). |
