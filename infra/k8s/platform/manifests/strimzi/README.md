# Kafka data plane (Strimzi + Apicurio Registry)

OpenFoundry's event backbone is built exclusively on **OSS** components:

| Component        | License     | Role                                  |
|------------------|-------------|---------------------------------------|
| Strimzi Operator | Apache-2.0  | Manages the Kafka cluster             |
| Apache Kafka     | Apache-2.0  | Event broker (KRaft, no ZooKeeper)    |
| Apicurio Registry| Apache-2.0  | Schema Registry (Avro/Protobuf/JSON)  |
| CloudNativePG    | Apache-2.0  | PostgreSQL backend for Apicurio       |
| Cilium           | Apache-2.0  | Network policy enforcement            |

**Confluent Platform / Confluent Schema Registry is intentionally avoided** —
the Confluent Community License is not OSI-approved.

## Files

| File                              | Purpose                                                                                  |
|-----------------------------------|------------------------------------------------------------------------------------------|
| `kafka-cluster.yaml`              | `KafkaNodePool` (KRaft combined controller+broker) and `Kafka` CR with HA defaults       |
| `kafka-topics.yaml`               | Base data-plane `KafkaTopic`s (12 partitions, RF=3, `min.insync.replicas=2`)             |
| `apicurio-registry.yaml`          | Apicurio namespace, CNPG `Cluster` for Postgres backend, bootstrap Secret                |
| `network-policies.yaml`           | `CiliumNetworkPolicy`s scoping producers/consumers per service                           |
| `values-strimzi-operator.yaml`    | Helm values for the upstream `strimzi/strimzi-kafka-operator` chart                      |
| `values-apicurio-registry.yaml`   | Helm values for the upstream `apicurio/apicurio-registry` chart                          |

The Strimzi and Apicurio **operators / charts** themselves are not packaged in
this repository — they are installed from upstream Helm repositories using the
values files above. Only the *desired CRs* are checked in here, mirroring the
convention used by `infra/k8s/platform/manifests/rook`.

## Apply order

```bash
# 0. (Prereqs) CloudNativePG and Cilium are assumed to be already installed
#    cluster-wide. Cilium must run in CNI mode with kube-proxy replacement.

# 1. Strimzi operator (cluster-scoped CRDs, namespace-scoped controller).
helm repo add strimzi https://strimzi.io/charts/
helm install -n kafka --create-namespace strimzi-operator \
  strimzi/strimzi-kafka-operator \
  -f infra/k8s/platform/manifests/strimzi/values-strimzi-operator.yaml

# 2. Kafka cluster (KRaft, JBOD on ceph-rbd).
kubectl apply -f infra/k8s/platform/manifests/strimzi/kafka-cluster.yaml
kubectl -n kafka wait --for=condition=Ready kafka/openfoundry --timeout=15m

# 3. Topics.
kubectl apply -f infra/k8s/platform/manifests/strimzi/kafka-topics.yaml

# 4. Apicurio Postgres backend (CNPG) and namespace.
kubectl apply -f infra/k8s/platform/manifests/strimzi/apicurio-registry.yaml
kubectl -n apicurio wait --for=condition=Ready cluster.postgresql.cnpg.io/apicurio-pg --timeout=10m

# 5. Apicurio Registry itself.
helm repo add apicurio https://apicurio.github.io/apicurio-registry/charts/
helm install -n apicurio apicurio-registry \
  apicurio/apicurio-registry \
  -f infra/k8s/platform/manifests/strimzi/values-apicurio-registry.yaml

# 6. NetworkPolicies (default-deny + allow lists).
kubectl apply -f infra/k8s/platform/manifests/strimzi/network-policies.yaml
```

## Validation

```bash
# Lint Helm values against the upstream charts.
helm template -n kafka strimzi-operator \
  strimzi/strimzi-kafka-operator -f values-strimzi-operator.yaml >/dev/null
helm template -n apicurio apicurio-registry \
  apicurio/apicurio-registry -f values-apicurio-registry.yaml >/dev/null

# Validate the raw manifests against the API server schema.
kubectl --dry-run=client apply -f kafka-cluster.yaml
kubectl --dry-run=client apply -f kafka-topics.yaml
kubectl --dry-run=client apply -f apicurio-registry.yaml
kubectl --dry-run=client apply -f network-policies.yaml
```

## Rack/zone awareness and fetch-from-follower

`kafka-cluster.yaml` configures `spec.kafka.rack.topologyKey:
topology.kubernetes.io/zone`, so Strimzi injects each broker's failure-domain
zone as its `broker.rack`. Combined with the existing pod
`topologySpreadConstraints` on `topology.kubernetes.io/zone`, this guarantees
that the three replicas of every partition land in distinct AZs, preserving
the durability contract (`min.insync.replicas=2`,
`default.replication.factor=3`, `unclean.leader.election.enable=false`) even
when a whole zone fails.

In addition, `replica.selector.class` is set to
`org.apache.kafka.common.replica.RackAwareReplicaSelector`, which enables
Kafka's *fetch-from-follower* path (KIP-392): consumers that advertise a
`client.rack` matching a follower's rack will be served by that in-zone
follower instead of the cross-AZ leader. This trims the consumer-tail latency
budget allocated to Kafka in
[`docs/architecture/adr/ADR-0012-data-plane-slos.md`](../../../../../docs/architecture/adr/ADR-0012-data-plane-slos.md)
by removing inter-AZ hops on the hot read path, and it also reduces inter-AZ
egress costs without compromising durability (writes still go to the leader
and respect `acks=all` + `min.insync.replicas=2`). Storage, JBOD layout and
listener/auth (SASL/TLS) configuration are intentionally untouched.

## Operating the cluster

See [`infra/runbooks/kafka.md`](../../../../runbooks/kafka.md) for rolling upgrades,
broker (node-pool) expansion, and partition reassignment procedures.
