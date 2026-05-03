# Helm release layout — split releases only

> Companion to [ADR-0031](../../docs/architecture/adr/ADR-0031-helm-chart-split-five-releases.md).
> The umbrella `open-foundry` chart was removed on **2026-05-02** after
> the split releases reached operational parity.

## Releases

| Release | Path | Status |
| --- | --- | --- |
| `of-shared` (library chart) | [`of-shared/`](of-shared/) | active |
| `of-platform` | [`of-platform/`](of-platform/) | active |
| `of-data-engine` | [`of-data-engine/`](of-data-engine/) | active |
| `of-ontology` | [`of-ontology/`](of-ontology/) | active |
| `of-ml-aip` | [`of-ml-aip/`](of-ml-aip/) | active |
| `of-apps-ops` | [`of-apps-ops/`](of-apps-ops/) | active |

## Shared profiles

Cross-release environment posture now lives under
[`profiles/`](profiles/):

- `values-dev.yaml`
- `values-staging.yaml`
- `values-prod.yaml`
- `values-airgap.yaml`
- `values-multicloud.yaml`
- `values-sovereign-eu.yaml`
- `values-apollo.yaml`

Each release also owns its service-specific tuning in
`<release>/values-{dev,staging,prod}.yaml`.

## Install order

The platform layer in [`../platform/`](../platform/) must be installed
before these app releases. Inside the app layer, `of-platform` must be
installed first because it owns the shared Ingress, the
`openfoundry-platform-profile` ConfigMap, and the Apollo CronJob. The
remaining four releases declare `needs:` on it in
[`helmfile.yaml.gotmpl`](helmfile.yaml.gotmpl), so the orchestrator
enforces the app order automatically.

The supported entrypoint is **helmfile**:

```sh
cd infra/k8s/platform && helmfile -e prod apply
cd infra/k8s/helm && helmfile -e prod apply
```

Layered postures (combine a base profile with a posture overlay) are
pre-declared as helmfile environments:

```sh
cd infra/k8s/helm && helmfile -e airgap       apply  # prod + airgap overlay
cd infra/k8s/helm && helmfile -e sovereign-eu apply  # prod + EU residency
cd infra/k8s/helm && helmfile -e multicloud   apply
cd infra/k8s/helm && helmfile -e apollo       apply
```

Equivalent manual commands (kept as escape hatch — not required day-to-day):

```sh
helm upgrade --install of-platform \
  infra/k8s/helm/of-platform \
  -f infra/k8s/helm/of-platform/values.yaml \
  -f infra/k8s/helm/profiles/values-prod.yaml \
  -f infra/k8s/helm/of-platform/values-prod.yaml

helm upgrade --install of-data-engine \
  infra/k8s/helm/of-data-engine \
  -f infra/k8s/helm/of-data-engine/values.yaml \
  -f infra/k8s/helm/profiles/values-prod.yaml \
  -f infra/k8s/helm/of-data-engine/values-prod.yaml

helm upgrade --install of-ontology \
  infra/k8s/helm/of-ontology \
  -f infra/k8s/helm/of-ontology/values.yaml \
  -f infra/k8s/helm/profiles/values-prod.yaml \
  -f infra/k8s/helm/of-ontology/values-prod.yaml

helm upgrade --install of-ml-aip \
  infra/k8s/helm/of-ml-aip \
  -f infra/k8s/helm/of-ml-aip/values.yaml \
  -f infra/k8s/helm/profiles/values-prod.yaml \
  -f infra/k8s/helm/of-ml-aip/values-prod.yaml

helm upgrade --install of-apps-ops \
  infra/k8s/helm/of-apps-ops \
  -f infra/k8s/helm/of-apps-ops/values.yaml \
  -f infra/k8s/helm/profiles/values-prod.yaml \
  -f infra/k8s/helm/of-apps-ops/values-prod.yaml
```

## Validation

Local bundle validation:

```sh
just helm-check
```

Direct render:

```sh
cd infra/k8s/helm && helmfile -e prod template > /tmp/openfoundry-prod.yaml
```

## Notes

- The bash bundle scripts under `bin/` (`upgrade-split-releases.sh`,
  `template-split-releases.sh`) were removed on **2026-05-03** in favour
  of the declarative [`helmfile.yaml.gotmpl`](helmfile.yaml.gotmpl).
- The shared Postgres Secret contract moved to
  [`DATABASE_URL.md`](DATABASE_URL.md).
- Vespa is deployed from the platform Helmfile in
  [`../platform/`](../platform/). The chart source now lives under
  [`../platform/charts/vespa/`](../platform/charts/vespa/).
