# Getting started

This section is the contributor entry point for the OpenFoundry documentation set.

## Start here

- [Repository map](/guide/repository-map) for the fastest path through the monorepo.
- [Local development](/guide/local-development) for day-to-day setup and commands.
- [Quality gates](/guide/quality-gates) for CI expectations and validation flows.
- [Documentation website](/guide/documentation-website) for authoring and publishing this docs site.

## Deploying to Kubernetes (post-S8 layout)

OpenFoundry now ships as **five Helm releases** plus the
`of-shared` library chart. See
[ADR-0031](/architecture/adr/ADR-0031-helm-chart-split-five-releases)
and [`infra/k8s/helm/MIGRATION.md`](https://github.com/diocrafts/OpenFoundry/blob/main/infra/k8s/helm/MIGRATION.md):

- `of-platform` — gateway, identity, authz, tenancy.
- `of-data-engine` — connectors, ingestion, datasets, lineage, pipeline, SQL/BI.
- `of-ontology` — ontology definition/actions/query, object-database, sinks.
- `of-ml-aip` — model catalog/deployment, agent runtime, LLM, retrieval, eval.
- `of-apps-ops` — apps, notebook, exploratory, workflow, audit, telemetry, federation, code repo, SDK, entity-resolution.

The legacy umbrella chart was removed on **2026-05-02**. Deployments
now go through the five split charts plus the shared profile overlays
under `infra/k8s/helm/profiles/`.

## Why this section exists

The capability model used across the rest of the documentation is helpful once you know the platform shape. This section keeps a simpler contributor-first path for people who need to understand the repo before they reason about product capability domains.
