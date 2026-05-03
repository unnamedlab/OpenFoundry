# OpenFoundry Kubernetes

This directory is split into two installation layers. The top level stays
small on purpose: application Helm lives in `helm/`, third-party
platform infrastructure lives in `platform/`, and test-only assets live
in `bench/` and `chaos/`.

## Layers

| Layer | Path | Owns |
| --- | --- | --- |
| Platform | [`platform/`](platform/) | Third-party platform releases, operator CRs, bootstrap manifests, runtime packages |
| Apps | [`helm/`](helm/) | OpenFoundry application releases: `of-platform`, `of-data-engine`, `of-ontology`, `of-ml-aip`, `of-apps-ops` |

The legacy umbrella chart `helm/open-foundry` has been removed. New
services belong in one of the five app releases; third-party dependencies
belong in `platform/`.

## Platform Layout

| Path | Owns |
| --- | --- |
| [`platform/charts/`](platform/charts/) | Vendored third-party Helm charts used directly by the platform Helmfile |
| [`platform/values/`](platform/values/) | Per-profile platform overlays |
| [`platform/manifests/`](platform/manifests/) | Operator CRs, upstream chart values, bootstrap Jobs, and Kubernetes manifests |
| [`platform/packages/`](platform/packages/) | Runtime source packages bundled by platform charts, e.g. Vespa app package |

## Install

Install platform services first:

```sh
cd infra/k8s/platform
helmfile -e prod apply
```

Then install OpenFoundry apps:

```sh
cd infra/k8s/helm
helmfile -e prod apply
```

For offline rendering without Prometheus Operator CRDs in the current
kube context, render platform with:

```sh
cd infra/k8s/platform
helmfile -e prod template --args "--api-versions monitoring.coreos.com/v1/PodMonitor"
```

## Profiles

App profiles live under [`helm/profiles/`](helm/profiles/). Platform
service overlays live under [`platform/values/`](platform/values/);
base values and supporting CRs live under
[`platform/manifests/`](platform/manifests/).

`dev` keeps heavy platform services disabled. `staging` enables Vespa.
`prod` enables Vespa, Trino, Spark Operator and Mimir.
