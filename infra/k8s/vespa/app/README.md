# Vespa application package — canonical source

This directory contains the **versioned source of truth** for the Vespa
application package deployed by the
`infra/k8s/helm/open-foundry/charts/vespa/` Helm subchart.

```
app/
├── services.xml          # cluster topology (admin / container / content)
├── hosts.xml             # host-alias → DNS mapping (StatefulSet pod-DNS)
└── schemas/
    └── document.sd       # hybrid (BM25 + dense ANN/HNSW) schema
```

## How it is consumed

The Helm subchart keeps an **identical mirror** of these files under
`charts/vespa/files/` — Helm's `.Files.Get` is sandboxed to the chart
directory, so the mirror is the only way to bundle them into the
application-package `ConfigMap`.

When you change anything in this directory **you must keep the mirror in
sync**.  A `helm template` smoke-test will catch most regressions; the
runbook `infra/runbooks/vespa.md` documents the workflow.

## Standalone deploy (without Helm)

```bash
( cd infra/k8s/vespa/app && zip -r /tmp/vespa-app.zip . )
kubectl -n openfoundry port-forward svc/open-foundry-vespa-configserver 19071:19071 &
curl -s --header Content-Type:application/zip \
     --data-binary @/tmp/vespa-app.zip \
     http://localhost:19071/application/v2/tenant/default/prepareandactivate \
     | jq .
```

The `hosts.xml` shipped here assumes namespace `openfoundry` and Helm
release name `open-foundry`.  If you deploy under different names,
override the host entries before zipping.

## Cluster shape

| Cluster      | Role                  | Nodes | Notes                          |
|--------------|-----------------------|-------|--------------------------------|
| `admin`      | configserver / ZK     | 3     | Quorum, PDB `minAvailable=2`   |
| `default`    | stateless container   | 2     | Query + feed entry-point       |
| `documents`  | stateful content      | 3     | `redundancy=2`, PDB `minAvailable=2` |
