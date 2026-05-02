# OpenFoundry Infrastructure Profiles

This directory contains the deployment artifacts used to run OpenFoundry locally, in Kubernetes, and through Terraform-based infrastructure workflows.

## Deployment modes

> **S8.2 update** — the umbrella `k8s/helm/open-foundry` chart is
> deprecated (removal date 2026-08-01). New work targets the five
> release-aligned charts; see
> [`k8s/helm/MIGRATION.md`](k8s/helm/MIGRATION.md) and
> [ADR-0031](../docs/architecture/adr/ADR-0031-helm-chart-split-five-releases.md).
>
> ```bash
> helm upgrade --install of-platform    k8s/helm/of-platform    -f k8s/helm/of-platform/values.yaml
> helm upgrade --install of-data-engine k8s/helm/of-data-engine -f k8s/helm/of-data-engine/values.yaml
> helm upgrade --install of-ontology    k8s/helm/of-ontology    -f k8s/helm/of-ontology/values.yaml
> helm upgrade --install of-ml-aip      k8s/helm/of-ml-aip      -f k8s/helm/of-ml-aip/values.yaml
> helm upgrade --install of-apps-ops    k8s/helm/of-apps-ops    -f k8s/helm/of-apps-ops/values.yaml
> ```

- `docker-compose.yml`: local control-plane dependencies plus optional `app` profile for `auth-service`, `gateway`, `web`, and an `nginx` edge proxy, with image overrides such as `OPENFOUNDRY_POSTGRES_IMAGE` for mirrored or air-gapped registries.
- `k8s/helm/open-foundry/values-multicloud.yaml`: multi-cloud SaaS topology with workload identity and Apollo-driven gated fleet sync.
- `k8s/helm/open-foundry/values-airgap.yaml`: air-gapped / sovereign deployment posture with private registry mirroring and public-egress shutdown.
- `k8s/helm/open-foundry/values-sovereign-eu.yaml`: EU-only residency profile with ingress allowlists, node residency labels, and egress fencing.
- `k8s/helm/open-foundry/values-apollo.yaml`: autonomous CI/CD profile that reconciles rollout fleets through the existing platform APIs.

## Render examples

```bash
helm template open-foundry infra/k8s/helm/open-foundry \
  -f infra/k8s/helm/open-foundry/values.yaml \
  -f infra/k8s/helm/open-foundry/values-multicloud.yaml
```

```bash
helm template open-foundry infra/k8s/helm/open-foundry \
  -f infra/k8s/helm/open-foundry/values.yaml \
  -f infra/k8s/helm/open-foundry/values-airgap.yaml
```

## Terraform

`infra/terraform/providers/openfoundry/` now models deployment cells, air-gap bundles, geo-fence policies and Apollo rollouts in addition to repository, audit and Nexus resources.

## Runbooks

- `runbooks/disaster-recovery.md`: ordered DR recovery procedure for Compose and Kubernetes
- `runbooks/upgrade-playbook.md`: canary, promotion and rollback checklist

## Backup scripts

- `scripts/postgres_backup.sh`
- `scripts/postgres_restore.sh`
- `scripts/minio_backup.sh`
- `scripts/minio_restore.sh`
