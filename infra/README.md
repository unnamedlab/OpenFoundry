# OpenFoundry Infrastructure Profiles

This directory contains the deployment artifacts used to run OpenFoundry locally, in Kubernetes, and through Terraform-based infrastructure workflows.

## Deployment modes

> **S8.6 update** — the bash bundle scripts under `k8s/helm/bin/` were
> removed on 2026-05-03 and replaced by a single declarative
> [`helmfile.yaml.gotmpl`](k8s/helm/helmfile.yaml.gotmpl). Kubernetes
> delivery now has a platform layer plus the five release-aligned app
> charts; see
> [`k8s/helm/MIGRATION.md`](k8s/helm/MIGRATION.md) and
> [ADR-0031](../docs/architecture/adr/ADR-0031-helm-chart-split-five-releases.md).
>
> ```bash
> cd infra/k8s/platform && helmfile -e prod apply
> cd infra/k8s/helm && helmfile -e prod apply
> ```

- `docker-compose.yml`: local control-plane dependencies plus optional `app` profile for `auth-service`, `gateway`, `web`, and an `nginx` edge proxy, with image overrides such as `OPENFOUNDRY_POSTGRES_IMAGE` for mirrored or air-gapped registries.
- `local/`: support files mounted by Compose (`postgres-init/`, `nginx/`).
- `k8s/platform/observability/`: platform-owned Prometheus rules, Grafana dashboards, and monitor CRs.
- `k8s/helm/profiles/values-multicloud.yaml`: multi-cloud SaaS topology with workload identity and Apollo-driven gated fleet sync.
- `k8s/helm/profiles/values-airgap.yaml`: air-gapped / sovereign deployment posture with private registry mirroring and public-egress shutdown.
- `k8s/helm/profiles/values-sovereign-eu.yaml`: EU-only residency profile with ingress allowlists, node residency labels, and egress fencing.
- `k8s/helm/profiles/values-apollo.yaml`: autonomous CI/CD profile that reconciles rollout fleets through the existing platform APIs.

## Render examples

```bash
cd infra/k8s/helm && helmfile -e multicloud template > /tmp/openfoundry-multicloud.yaml
```

```bash
cd infra/k8s/helm && helmfile -e airgap template > /tmp/openfoundry-airgap.yaml
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
