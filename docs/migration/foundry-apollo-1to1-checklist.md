# Foundry Apollo (cross-environment promotion) 1:1 parity checklist

Date: 2026-05-17
Scope: public-docs-based parity plan for OpenFoundry's Apollo-equivalent
cross-environment promotion layer: environment topology, promotion
pipelines, product bundles as portable units, dependency resolution and
ordering, environment-pinned configuration, region and compliance
boundaries, gated approvals, atomic rollback per product, audit, and
integrations with the Marketplace, Functions runtime, OSDK, ontology
migrations, dataset migrations, and schedule lifecycle.

> **Scope distinction.** This checklist covers the **cross-environment
> promotion** plane (dev → staging → prod, multi-region, multi-tenant
> rollouts). It depends on bundles and the Marketplace product format
> defined in
> [foundry-devops-marketplace-1to1-checklist.md](./foundry-devops-marketplace-1to1-checklist.md)
> and re-uses the Marketplace install pipeline as the per-environment
> applicator. It does **not** redefine product manifests; it defines the
> promotion-pipeline + boundary + approval semantics that sit above them.

This document is intentionally implementation-oriented. It does not attempt
to clone Palantir branding, private source code, proprietary assets,
screenshots, or any non-public behavior. The target is **functional parity
based on public Palantir Foundry documentation**.

## Parity scope boundary

All checklist work is governed by the
[Foundry public-docs parity policy](../reference/foundry-public-docs-parity-policy.md).
OpenFoundry may implement behavior described in public Palantir documentation,
but contributors must not copy private source, decompile bundles, import
tenant-specific exports, use Palantir branding, or reuse proprietary assets.

## Status vocabulary

| Status | Meaning |
| --- | --- |
| `todo` | Not implemented or not yet verified in OpenFoundry. |
| `partial` | Some surface exists, but behavior is incomplete or not wired end-to-end. |
| `blocked` | Requires a platform dependency, public documentation, or product decision. |
| `done` | Implemented, tested, documented, and verified through UI or API smoke tests. |

## Priority vocabulary

| Priority | Meaning |
| --- | --- |
| `P0` | Required for credible promotion: environment topology, promotion of a product from dev to prod with audit and rollback. |
| `P1` | Required for Foundry-style parity: dependency-ordered multi-product promotions, gated approvals, environment-pinned config, drift detection. |
| `P2` | Advanced parity: multi-region/compliance-boundary promotions, canary rollouts, scheduled promotions, observability and SLOs. |

## Official Palantir documentation library

### Product overview

- [Apollo overview](https://palantir.com/docs/apollo/overview)
- [DevSecOps with Apollo](https://palantir.com/docs/apollo/devsecops)
- [Foundry DevOps supported resources](https://www.palantir.com/docs/foundry/foundry-devops/supported-resources)

### Concepts

- [Environments and channels](https://palantir.com/docs/apollo/environments)
- [Products and releases](https://palantir.com/docs/apollo/products)
- [Promotion pipelines](https://palantir.com/docs/apollo/pipelines)
- [Approvals and gates](https://palantir.com/docs/apollo/approvals)
- [Rollback](https://palantir.com/docs/apollo/rollback)

### Integration

- [Marketplace promotion](https://palantir.com/docs/foundry/marketplace/promotion)
- [Region and residency](https://palantir.com/docs/apollo/regions)

## Milestone A: credible promotion of a single product across environments

### Environment topology

- [ ] `APO.1` Environment resource (`P0`, `todo`)
  - CRUD an `environment` resource with: name, kind (dev/staging/prod/custom), region, compliance boundary tag (e.g. fedramp-moderate, gdpr-eu), enrollment id, and base URL.
  - Environments are first-class Compass resources with markings and per-role permissions.
  - Docs: [Environments and channels](https://palantir.com/docs/apollo/environments).

- [ ] `APO.2` Channel resource (`P0`, `todo`)
  - A `channel` is an ordered list of environments (e.g. `dev → staging → prod`).
  - Channels can branch (e.g. `dev → staging-eu → prod-eu` and `dev → staging-us → prod-us`).
  - Docs: [Environments and channels](https://palantir.com/docs/apollo/environments).

### Product and release model

- [ ] `APO.3` Product release pointer (`P0`, `todo`)
  - For each Marketplace product, track which release is installed in each environment with: release version, install timestamp, installer, status (healthy/degraded/failed).
  - Docs: [Products and releases](https://palantir.com/docs/apollo/products).

- [ ] `APO.4` Release immutability (`P0`, `todo`)
  - A released product bundle is content-addressed (content hash) and immutable.
  - Promotion always installs the same bytes across environments; rebuilds are explicitly versioned.
  - Docs: [Products and releases](https://palantir.com/docs/apollo/products).

### Promotion pipeline

- [ ] `APO.5` Promotion pipeline resource (`P0`, `todo`)
  - CRUD a `promotion_pipeline` per product (or per product group) bound to a channel; default stages match the channel.
  - Per-stage configuration: pre-install checks, env-pinned config overrides, approvers, post-install verification.
  - Docs: [Promotion pipelines](https://palantir.com/docs/apollo/pipelines).

- [ ] `APO.6` Promote a release through the pipeline (`P0`, `todo`)
  - `POST /pipelines/{rid}/promote` with a release version starts a promotion run; the run records per-stage status, logs, and the resulting environment state.
  - Stages run sequentially; failures pause the pipeline and surface a remediation panel.
  - Docs: [Promotion pipelines](https://palantir.com/docs/apollo/pipelines).

### Rollback

- [ ] `APO.7` Per-environment rollback (`P0`, `todo`)
  - Roll back a product in one environment to a prior installed release with one click.
  - Rollback is atomic at the product level (all resources in the bundle, not a single dataset/widget).
  - Docs: [Rollback](https://palantir.com/docs/apollo/rollback).

## Milestone B: dependency ordering, approvals, env-pinned config, drift

### Dependency ordering

- [ ] `APO.8` Cross-product dependency resolution (`P1`, `todo`)
  - Products may depend on other products at specific minimum versions; promotion plans resolve the install order and refuse promotions that would break dependents.
  - Docs: [Promotion pipelines](https://palantir.com/docs/apollo/pipelines).

- [ ] `APO.9` Atomic multi-product promotion (`P1`, `todo`)
  - A promotion plan can include multiple related products; either all succeed or the environment is rolled back to the prior consistent state.
  - Docs: [Promotion pipelines](https://palantir.com/docs/apollo/pipelines).

### Approvals and gates

- [ ] `APO.10` Approver groups per stage (`P1`, `todo`)
  - Each pipeline stage configurable with one or more approver groups; promotion to that stage blocks until enough approvals are recorded.
  - Approvals auditable and timestamped with the reviewer's marking clearances at decision time.
  - Docs: [Approvals and gates](https://palantir.com/docs/apollo/approvals).

- [ ] `APO.11` Automated pre-install checks (`P1`, `todo`)
  - Per-stage checks: compatibility with current environment state (ontology version, dataset schemas, OSDK consumers), dependency satisfaction, marking compatibility.
  - Failures block promotion with a remediation panel.
  - Docs: [Approvals and gates](https://palantir.com/docs/apollo/approvals).

- [ ] `APO.12` Post-install verification (`P1`, `todo`)
  - Per-stage checks after install: dataset builds green, schedules reactivated, OSDK published, agent runs healthy, no new data-health regressions.
  - Failures trigger auto-rollback if configured.
  - Docs: [Promotion pipelines](https://palantir.com/docs/apollo/pipelines).

### Environment-pinned configuration

- [ ] `APO.13` Config overlay per environment (`P1`, `todo`)
  - Bundles declare configurable parameters (secrets, endpoints, feature flags) with types and defaults.
  - Per environment, the overlay maps parameters to values from a secret store or env config.
  - Promotion never copies secrets across environments; each environment supplies its own.
  - Docs: [Products and releases](https://palantir.com/docs/apollo/products).

- [ ] `APO.14` Schedule and trigger reactivation policy (`P1`, `todo`)
  - On install, schedules and triggers default to **paused** in non-prod and **active** in prod (configurable per pipeline stage).
  - Side-effect destinations (webhooks, notifications) honor the environment's safe-defaults policy from the Global Branching checklist's analog.
  - Docs: [Promotion pipelines](https://palantir.com/docs/apollo/pipelines).

### Drift detection

- [ ] `APO.15` Environment drift detection (`P1`, `todo`)
  - Continuously compare the installed product's expected state vs. the live environment state (dataset schemas, ontology versions, action types, function impls).
  - Surface drift in the Apollo UI with one-click reconcile or one-click re-promote.
  - Docs: [Products and releases](https://palantir.com/docs/apollo/products).

## Milestone C: regions, canary, observability, scheduling

### Region and compliance boundaries

- [ ] `APO.16` Region-locked promotion (`P2`, `todo`)
  - Pipelines that cross region/compliance boundaries require explicit allow-listed approval and emit a flagged audit event.
  - Bundles can declare data-residency requirements; promotions into a non-matching region fail.
  - Docs: [Region and residency](https://palantir.com/docs/apollo/regions).

- [ ] `APO.17` Compliance boundary tags (`P2`, `todo`)
  - Environments tagged with compliance boundaries (FedRAMP, ITAR, GDPR-EU, HIPAA, etc.); product manifests declare which boundaries they can run in.
  - Mismatched promotions blocked with a clear policy message.
  - Docs: [Region and residency](https://palantir.com/docs/apollo/regions).

### Canary and progressive rollout

- [ ] `APO.18` Canary stage (`P2`, `todo`)
  - A pipeline stage can be configured as canary: install to a subset of consumers (subset of users, subset of projects, percent rollout) and monitor for a configurable window before progressing.
  - Docs: [Promotion pipelines](https://palantir.com/docs/apollo/pipelines).

- [ ] `APO.19` Auto-rollback on regression (`P2`, `todo`)
  - Canary stages can subscribe to data-health, error-rate, and SLO signals; regressions trigger auto-rollback and pause the pipeline.
  - Docs: [Rollback](https://palantir.com/docs/apollo/rollback).

### Scheduling and notifications

- [ ] `APO.20` Scheduled promotions (`P2`, `todo`)
  - Schedule a promotion at a future time (e.g., off-hours maintenance window) with pre-flight gating that re-runs at scheduled time.
  - Docs: [Promotion pipelines](https://palantir.com/docs/apollo/pipelines).

- [ ] `APO.21` Promotion notifications (`P2`, `todo`)
  - Subscribe roles/users to pipeline events (started, awaiting approval, succeeded, failed, rolled back) via Pulse notifications.
  - Docs: [Approvals and gates](https://palantir.com/docs/apollo/approvals).

### Observability and SLOs

- [ ] `APO.22` Promotion run trace and logs (`P2`, `todo`)
  - Each promotion run emits an OTel trace covering all stages with per-resource install spans; logs streamable in the Apollo UI.
  - Docs: [Promotion pipelines](https://palantir.com/docs/apollo/pipelines).

- [ ] `APO.23` Pipeline SLOs (`P2`, `todo`)
  - Per-pipeline SLO metrics: lead time, deploy frequency, change-failure rate, MTTR.
  - Dashboard surfaced in Apollo admin.
  - Docs: [Apollo overview](https://palantir.com/docs/apollo/overview).

### Audit

- [ ] `APO.24` Comprehensive audit (`P2`, `todo`)
  - Every promotion, approval, rollback, drift event, and canary decision is recorded in the central audit trail with actor, environment, product, release, and reason.
  - Audit consumable from the Security/Governance checklist's audit query surface.
  - Docs: [Approvals and gates](https://palantir.com/docs/apollo/approvals).

## Implementation inventory to collect before coding

- [ ] `INV.1` Identify the Marketplace bundle install pipeline that Apollo will call as the per-environment applicator.
- [ ] `INV.2` Identify the secret store and config overlay path per environment.
- [ ] `INV.3` Identify the data-health, error-rate, and SLO signals consumable for canary auto-rollback.
- [ ] `INV.4` Identify the approval workflow (overlap with `workflow-automation-service/internal/approvals`).
- [ ] `INV.5` Identify the audit emission path (overlap with Security/Governance).
- [ ] `INV.6` Identify the cross-region transport (federation-product-exchange-service) and verify region-locked semantics.
- [ ] `INV.7` Produce a parity matrix sibling JSON entry once a first implementation inventory is in place.

## Suggested service boundaries

| Surface | Responsibilities |
| --- | --- |
| `apollo-control-service` | Environment/channel/pipeline CRUD, promotion run state machine, drift detector, SLO computation. |
| `apollo-applicator-service` | Per-environment installer that wraps the Marketplace install pipeline with config overlay, pre-/post-checks, and atomic rollback. |
| `federation-product-exchange-service` | Cross-region transport of immutable product bundles (existing service, extended to honor region tags). |
| `workflow-automation-service` | Approval steps and notification fan-out for pipeline gates. |
| `apps/web` | Apollo UI: environment map, product release matrix, pipeline runs, approvals, drift, canary controls, audit. |
