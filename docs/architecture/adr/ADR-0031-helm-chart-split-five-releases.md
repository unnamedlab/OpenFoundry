# ADR-0031 — Helm chart split into 5 releases

| Field | Value |
| --- | --- |
| Status | Accepted |
| Date | 2026-05-02 |
| Stream | S8.2 (cleanup & hardening) |
| Related | [ADR-0030](ADR-0030-service-consolidation-30-targets.md), [ADR-0011](ADR-0011-control-vs-data-bus-contract.md) |

## Context

Before the split, every OpenFoundry service was a sub-chart of a single
umbrella release now removed from the repo.
A Helm release of that chart spans 97 Deployments + Services + HPAs +
PDBs + NetworkPolicies. In production this means:

* `helm upgrade` is an all-or-nothing transaction. A bad change to one
  service's NetworkPolicy can roll back unrelated services.
* `helm template` output is > 50 000 lines and CI lint takes minutes.
* CODEOWNERS for `values.yaml` is everyone, which makes review
  ineffective.
* DR cutover (S7.5) requires a full umbrella `--reuse-values` upgrade
  even when the change is to two env vars.

Splitting the umbrella into release-aligned charts lets each oncall
rotation own its release cadence and shrinks the blast radius of a
bad upgrade.

## Decision

Split the umbrella `open-foundry` chart into **five top-level Helm
releases** under `infra/k8s/helm/`. Each release groups services that
share an oncall rotation, a release cadence and a blast radius.

| Release | Services (post-S8.1) | Oncall |
| --- | --- | --- |
| `of-platform` | `edge-gateway-service`, `identity-federation-service`, `authorization-policy-service`, `tenancy-organizations-service` | platform |
| `of-data-engine` | `connector-management-service`, `ingestion-replication-service`, `dataset-versioning-service`, `lineage-service`, `pipeline-build-service`, `sql-bi-gateway-service` | data-engineering |
| `of-ontology` | `ontology-definition-service`, `ontology-actions-service`, `ontology-query-service`, `object-database-service`, `ontology-indexer` (sink) | ontology |
| `of-ml-aip` | `model-catalog-service`, `model-deployment-service`, `agent-runtime-service`, `llm-catalog-service`, `retrieval-context-service`, `ai-evaluation-service`, `ai-sink` (sink) | ai |
| `of-apps-ops` | `application-composition-service`, `notebook-runtime-service`, `ontology-exploratory-analysis-service`, `solution-design-service`, `workflow-automation-service`, `notification-alerting-service`, `audit-compliance-service`, `audit-sink` (sink), `telemetry-governance-service`, `federation-product-exchange-service`, `code-repository-review-service`, `sdk-generation-service`, `entity-resolution-service` | apps + ops |

### Layout

```
infra/k8s/helm/
├── of-platform/
│   ├── Chart.yaml
│   ├── values.yaml
│   ├── values-prod.yaml
│   ├── values-dev.yaml
│   └── templates/
├── of-data-engine/
├── of-ontology/
├── of-ml-aip/
├── of-apps-ops/
├── profiles/             (shared env posture: prod/staging/dev/airgap/...)
├── of-shared/            (Helm library chart; templates referenced by the 5 releases above)
│   ├── Chart.yaml        (type: library)
│   └── templates/
│       ├── _service.tpl
│       ├── _deployment.tpl
│       ├── _hpa.tpl
│       ├── _pdb.tpl
│       └── _networkpolicy.tpl
```

### Shared library chart

Every release depends on the `of-shared` library chart for the
boilerplate templates that were duplicated across services in the
umbrella. The library chart exports `define`s consumed by the
release-level `templates/services.yaml` which loops over
`.Values.services`.

This keeps platform-wide changes (e.g. adding an OpenTelemetry
sidecar) to a single PR against `of-shared`.

### Cross-release contracts

* **Service discovery**: services keep using k8s DNS
  (`<svc>.openfoundry.svc.cluster.local`). Splits change nothing at
  the network level.
* **Shared ConfigMaps / Secrets**: `platform-profile`,
  `database-credentials`, `kafka-credentials` are produced by
  `of-platform` and consumed by the others via
  `valueFrom.secretKeyRef`. `of-platform` is therefore a hard
  dependency for the other four releases at install time (operator
  documents the order).
* **Ingress**: `of-platform` owns the single Ingress / Gateway API
  resource; the other releases register their routes via
  `HTTPRoute` referencing the shared Gateway.

### Values overlays

Each release owns its service-specific `values-{dev,staging,prod}.yaml`.
Cross-release posture lives in `infra/k8s/helm/profiles/values-{dev,
staging,prod,sovereign-eu,airgap,multicloud,apollo}.yaml` so one
environment change (ingress, air-gap posture, object store, Cassandra
failover pinning) can be applied uniformly across all five releases.

## Consequences

### Positive

* **Scoped blast radius**: a bad rollout of `of-ml-aip` cannot
  rollback `of-platform`.
* **Independent cadence**: data-engineering can ship daily without
  waiting for AI to be ready.
* **CODEOWNERS**: each release's `values-prod.yaml` has a real owning
  team.
* **Review ergonomics**: `helm template of-data-engine` output is ~ 1/5
  the size of the umbrella; CI lint is fast enough for pre-commit.

### Negative

* **5× more `helm upgrade` invocations** in CI/CD. Mitigated by
  parallel CD pipeline (one job per release).
* **Cross-release contract drift risk**. Mitigated by keeping the
  contract surface intentionally tiny (3 ConfigMaps + 1 Gateway) and
  by integration tests in `smoke/` that exercise the contract
  end-to-end.
* **Operational sequencing**: `of-platform` is a hard dependency for
  the other four releases because it owns the shared ingress and the
  `openfoundry-platform-profile` ConfigMap.

## Execution

1. Land the empty `of-shared` library chart.
2. Cut `of-platform` first (smallest scope, lowest risk).
3. Run both umbrella and `of-platform` in parallel for one week in
   staging; diff `helm template` output.
4. Switch staging to `of-platform`; remove its services from the
   umbrella values for staging.
5. Repeat (2–4) for each remaining release.
6. After all five releases are live in staging for two weeks, cut
   over prod release-by-release.
7. Delete the legacy umbrella after the split bundle reaches parity.

## Follow-up

The legacy umbrella was removed on **2026-05-02**; the operational
entrypoint is now [`infra/k8s/helm/bin/upgrade-split-releases.sh`](../../../infra/k8s/helm/bin/upgrade-split-releases.sh).

## References

* [ADR-0011 — Control vs data bus contract](ADR-0011-control-vs-data-bus-contract.md)
* [ADR-0030 — Service consolidation 97 → 30](ADR-0030-service-consolidation-30-targets.md)
* [`infra/k8s/helm/MIGRATION.md`](../../../infra/k8s/helm/MIGRATION.md)
