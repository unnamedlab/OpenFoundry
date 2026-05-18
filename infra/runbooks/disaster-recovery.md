# Disaster Recovery Runbook

Date: April 25, 2026

## Objective

Recover OpenFoundry after a partial or total loss of the control plane, minimizing RTO and avoiding inconsistent restores between PostgreSQL and object storage.

## Dependencies

- Recent PostgreSQL snapshots or dumps
- Backup of the main artifact/dataset bucket
- Credentials for the cluster or the Compose host
- Helm/Terraform manifests for the affected environment

## Recovery order

1. Restore network, DNS, registry, and base secrets
2. Restore PostgreSQL
3. Restore object storage
4. Bring up stateless services
5. Verify migrations
6. Resume schedulers, sync engines, and reconcilers
7. Run functional smoke checks

## Compose procedure

1. Stop schedulers and reconcilers to prevent new writes
2. Restore PostgreSQL with `infra/scripts/postgres_restore.sh`
3. Restore buckets with `infra/scripts/minio_restore.sh`
4. Bring up `docker compose` using the same profiles that were in use before the incident
5. Verify the health of:
   - `gateway`
   - `identity-federation-service`
   - `dataset-versioning-service` (current dataset/catalog owner; retired `data-asset-catalog-service` was a pre-consolidation name)
   - `ontology-definition-service`, `ontology-query-service`, and `ontology-actions-service` (the current ontology split; retired `ontology-service` was not a single current binary)
   - `federation-product-exchange-service` (current marketplace/product-exchange owner)

## Kubernetes procedure

1. Scale workloads with mutating logic or background jobs to `0`
2. Restore managed volumes or snapshots
3. Reapply the base chart and environment overlays
4. Re-enable cronjobs, reconcilers, and autoscaling
5. Re-run smoke checks and critical paths

## Mandatory smoke checks

- Login and token issuance
- Dataset listing
- Ontology object types listing
- Pipeline preview
- DevOps fleets query
- Chat or AI provider health, if the environment uses it

## Exit criteria

- All critical services `healthy`
- PostgreSQL restored with migrations aligned
- Object storage reachable and with the expected paths
- At least one valid `dataset -> ontology -> app` flow
- At least one test `fleet sync` blocked or permitted by gates as expected
