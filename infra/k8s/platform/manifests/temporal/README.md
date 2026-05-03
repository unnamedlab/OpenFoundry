# Apache Temporal cluster on OpenFoundry

This package bundles the production Helm overlays plus the bootstrap
artifacts to run **Apache Temporal** on top of the OpenFoundry
Cassandra cluster (`infra/k8s/platform/manifests/cassandra/`). It is the substrate for
Stream S2 of the Cassandra/Foundry-parity migration plan.

ADRs:
- [ADR-0020](../../../../../docs/architecture/adr/ADR-0020-cassandra-as-operational-store.md)
  — Cassandra as operational store.
- [ADR-0021](../../../../../docs/architecture/adr/ADR-0021-temporal-on-cassandra-go-workers.md)
  — Temporal on Cassandra with Go SDK workers.

Layout:

```
infra/k8s/platform/manifests/temporal/
├── README.md                  # this file
├── namespace.yaml             # `temporal` namespace + PodSecurity labels
├── helm-release.yaml          # Flux/Argo HelmRelease (chart 0.46+)
├── values-prod.yaml           # production values: HA, mTLS, Cassandra
├── values-dev.yaml            # in-cluster dev: smaller, mTLS off
├── cassandra-keyspaces-job.yaml   # creates temporal_persistence + temporal_visibility
├── ui-ingress.yaml            # Temporal UI behind edge-gateway with OIDC
└── servicemonitor.yaml        # Prometheus scrape for SDK + server metrics
```

## Provisioning order

The Helm chart owns the Temporal pods, but the Cassandra keyspaces
must exist beforehand because the `temporal-cassandra-tool` schema
upgrade Job runs on the first `helm install` and expects the keyspaces
present.

```bash
# 1. Cassandra cluster ready (assumed; see infra/k8s/platform/manifests/cassandra/README.md).
kubectl -n cassandra get k8ssandracluster of-cass-prod -o jsonpath='{.status.cassandraOperatorProgress}'
# expect: Ready

# 2. Create Temporal namespace + RBAC + secrets.
kubectl apply -f infra/k8s/platform/manifests/temporal/namespace.yaml

# 3. Create the keyspaces with NetworkTopologyStrategy {dc1:3, dc2:3, dc3:3}.
kubectl apply -f infra/k8s/platform/manifests/temporal/cassandra-keyspaces-job.yaml
kubectl -n temporal wait --for=condition=complete job/temporal-cassandra-keyspaces --timeout=5m

# 4. Apply the HelmRelease (Flux) or run helm install directly.
flux reconcile helmrelease -n temporal temporal --with-source
# or, plain helm:
helm repo add temporal https://go.temporal.io/helm-charts
helm install temporal temporal/temporal --version '~0.46.0' \
  -n temporal -f infra/k8s/platform/manifests/temporal/values-prod.yaml

# 5. UI behind the platform gateway with OIDC.
kubectl apply -f infra/k8s/platform/manifests/temporal/ui-ingress.yaml
```

The `cassandra-keyspaces-job.yaml` creates **only the keyspaces**, not
the schema. The chart's first run executes `temporal-cassandra-tool
setup-schema` against each keyspace. Subsequent upgrades execute
`update-schema`.

## Topology rationale

* **Frontend × 3** — gRPC entrypoint. Stateless. `PodAntiAffinity` on
  zone so the 3 replicas land in 3 different AZs.
* **History × 3** — owns workflow execution shards. Stateless from the
  pod's perspective (state in Cassandra), but ownership of shards is
  consistent-hashed; losing a pod triggers a rebalance.
* **Matching × 3** — task queue dispatch. Stateless.
* **Worker (system) × 3** — runs Temporal's **internal** workflows
  (replication, archival, scanner). **Business workers run in the
  separate `workers-go/` deployments**, NOT here (per ADR-0021).
* **No Cassandra-side schema replication tweaks** — Temporal's
  keyspaces use the same `NetworkTopologyStrategy {dc1:3, dc2:3,
  dc3:3}` as application keyspaces and observe the same
  `LOCAL_QUORUM` consistency level. ADR-0012 §A.3 thresholds apply.

## Authentication

* **Server↔Cassandra**: mTLS using the cert-manager-issued
  certificates from the Cassandra namespace (mounted as a volume; see
  `values-prod.yaml::server.config.persistence`).
* **Client↔Server**: mTLS optional, controlled by Linkerd mesh
  membership. Dev overlays disable mTLS entirely.
* **UI access**: routed through `edge-gateway-service` with OIDC
  against `identity-federation-service`. The chart's UI auth is
  disabled and replaced by an oauth2-proxy sidecar in
  `ui-ingress.yaml`.

## Observability

* **`ServiceMonitor`** (`servicemonitor.yaml`) scrapes the four
  service tiers + the Cassandra-tool job.
* **Grafana dashboard 17567** (Temporal SDK metrics) is provisioned
  via the central dashboards repo (`infra/k8s/platform/observability/grafana-dashboards/`).
  17567 covers worker-side metrics; the server-side dashboard is
  17570 (also added in the same repo).
* **Alerts** live next to the dashboards: see
  `infra/runbooks/temporal.md` for the alert→runbook mapping.

## Decommission / rollback

Temporal keyspaces are **append-mostly** with TTL (visibility
shorter than persistence). A clean uninstall is:

```bash
helm uninstall temporal -n temporal
# Keyspaces are NOT dropped automatically. To free space:
kubectl -n cassandra exec -it of-cass-prod-dc1-rack1-sts-0 -- cqlsh -e \
  "DROP KEYSPACE IF EXISTS temporal_persistence;
   DROP KEYSPACE IF EXISTS temporal_visibility;"
```

The drop is **irreversible** — the workflow history is gone. Take a
Medusa snapshot first if there is any chance of needing it back.
