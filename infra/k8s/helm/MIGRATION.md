# Helm release migration — umbrella `open-foundry` → 5 releases

> Companion to [ADR-0031](../../docs/architecture/adr/ADR-0031-helm-chart-split-five-releases.md).
> Tracks the rollout of the five new top-level Helm releases that
> replace the monolithic [`open-foundry`](open-foundry) umbrella.

## Releases (S8.2)

| Release | Path | Status |
| --- | --- | --- |
| `of-shared` (library chart) | [`of-shared/`](of-shared/) | scaffolded |
| `of-platform` | [`of-platform/`](of-platform/) | scaffolded |
| `of-data-engine` | [`of-data-engine/`](of-data-engine/) | scaffolded |
| `of-ontology` | [`of-ontology/`](of-ontology/) | scaffolded |
| `of-ml-aip` | [`of-ml-aip/`](of-ml-aip/) | scaffolded |
| `of-apps-ops` | [`of-apps-ops/`](of-apps-ops/) | scaffolded |
| `open-foundry` (legacy) | [`open-foundry/`](open-foundry/) | DEPRECATED — removal date **2026-08-01** |

## Install order

`of-platform` first (it owns the shared Gateway, ConfigMaps and
Secrets consumed by everything else), then the rest in any order:

```sh
helm upgrade --install of-platform   infra/k8s/helm/of-platform   -f infra/k8s/helm/of-platform/values-prod.yaml
helm upgrade --install of-data-engine infra/k8s/helm/of-data-engine -f infra/k8s/helm/of-data-engine/values-prod.yaml
helm upgrade --install of-ontology   infra/k8s/helm/of-ontology   -f infra/k8s/helm/of-ontology/values-prod.yaml
helm upgrade --install of-ml-aip     infra/k8s/helm/of-ml-aip     -f infra/k8s/helm/of-ml-aip/values-prod.yaml
helm upgrade --install of-apps-ops   infra/k8s/helm/of-apps-ops   -f infra/k8s/helm/of-apps-ops/values-prod.yaml
```

## Migration steps from the umbrella

1. **Stage 1 (staging only)** — Install `of-platform` *alongside* the
   umbrella. Verify both render identical Deployments for the
   platform services using
   `diff <(helm template of-platform infra/k8s/helm/of-platform) \
        <(helm template open-foundry infra/k8s/helm/open-foundry | grep -A1000 'name: edge-gateway-service')`.
2. **Stage 2** — Set `services.<platform-svc>.enabled: false` in the
   umbrella values for staging; the umbrella stops managing those
   Deployments. `of-platform` takes over.
3. **Stage 3** — Repeat for the other four releases.
4. **Stage 4 (prod)** — Apply the same sequence per environment.
5. **Stage 5** — Delete `infra/k8s/helm/open-foundry/` after every
   environment is on the split charts and the deprecation date is
   reached.

## Deprecation policy

The legacy umbrella stays installable until 2026-08-01. Any new
service must be added to the appropriate split release **only** —
the umbrella is feature-frozen.
