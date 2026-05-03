# Vespa application package — canonical source

This directory contains the **versioned source of truth** for the Vespa
application package deployed by the
`infra/k8s/platform/charts/vespa/` Helm chart.

```
vespa-app/
├── services.xml          # cluster topology (admin / container / content)
├── hosts.xml             # host-alias → DNS mapping (StatefulSet pod-DNS)
└── schemas/
    └── document.sd       # hybrid (BM25 + dense ANN/HNSW) schema
```

## How it is consumed

The platform Vespa chart keeps an **identical mirror** of these files
under `infra/k8s/platform/charts/vespa/files/` — Helm's `.Files.Get` is
sandboxed to the chart directory, so the mirror is the only way to bundle them into the
application-package `ConfigMap`.

When you change anything in this directory **you must keep the mirror in
sync**.  A `helm template` smoke-test will catch most regressions; the
runbook `infra/runbooks/vespa.md` documents the workflow.

## Standalone deploy (without Helm)

```bash
( cd infra/k8s/platform/packages/vespa-app && zip -r /tmp/vespa-app.zip . )
kubectl -n openfoundry port-forward svc/of-ontology-vespa-configserver-lb 19071:19071 &
curl -s --header Content-Type:application/zip \
     --data-binary @/tmp/vespa-app.zip \
     http://localhost:19071/application/v2/tenant/default/prepareandactivate \
     | jq .
```

The `hosts.xml` shipped here assumes namespace `openfoundry` and the
platform fullname `of-ontology-vespa`. If you deploy under different names,
override the host entries before zipping.

## Cluster shape

| Cluster      | Role                  | Nodes | Notes                          |
|--------------|-----------------------|-------|--------------------------------|
| `admin`      | configserver / ZK     | 3     | Quorum, PDB `minAvailable=2`   |
| `default`    | stateless container   | 2     | Query + feed entry-point       |
| `documents`  | stateful content      | 3     | `redundancy=2`, PDB `maxUnavailable=1` |

## High-availability manifests (PDB, topology spread, anti-affinity)

The runtime HA guarantees that back the `redundancy=2` / 3-node content
cluster declared in [`services.xml`](./services.xml) live in the platform
Helm chart, not in this directory:

- `PodDisruptionBudget` for content + configserver:
  [`charts/vespa/templates/poddisruptionbudgets.yaml`](../../charts/vespa/templates/poddisruptionbudgets.yaml)
- `topologySpreadConstraints` (zone-aware) and `podAntiAffinity`
  (preferred, per hostname) on the content `StatefulSet`:
  [`charts/vespa/templates/statefulset-content.yaml`](../../charts/vespa/templates/statefulset-content.yaml)
- Tunables (`vespa.content.podDisruptionBudget.maxUnavailable`,
  `vespa.topologySpreadConstraints.topologyKey`, `vespa.podAntiAffinity.*`)
  are exposed in
  [`charts/vespa/values.yaml`](../../charts/vespa/values.yaml)
  and can be overridden per environment (e.g. `values-prod.yaml`).

See [ADR-0007 — Search engine choice (Vespa only)](../../../../../docs/architecture/adr/ADR-0007-search-engine-choice.md)
for the rationale behind a 3-node, `redundancy=2` Vespa content cluster.
