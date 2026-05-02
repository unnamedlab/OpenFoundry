# OpenFoundry Architecture

The canonical technical documentation for this repository now lives in [`docs/`](docs/).

## Post-S8 service pyramid (≤ 30 services + 4 sinks, 5 Helm releases)

Stream S8 (cleanup & hardening) consolidates the original 97 service
crates down to 30 ownership boundaries, packaged as five Helm
releases. See
[`docs/architecture/adr/ADR-0030-service-consolidation-30-targets.md`](docs/architecture/adr/ADR-0030-service-consolidation-30-targets.md),
[`docs/architecture/adr/ADR-0031-helm-chart-split-five-releases.md`](docs/architecture/adr/ADR-0031-helm-chart-split-five-releases.md)
and the per-service status table in
[`docs/architecture/service-consolidation-map.md`](docs/architecture/service-consolidation-map.md).

```
                        ┌─────────────────────────┐
                        │   apps/web (SvelteKit)  │
                        └────────────┬────────────┘
                                     │
   ┌───────────────────────┬─────────┴─────────┬──────────────────────┬───────────────────────┐
   │  of-platform          │  of-data-engine   │  of-ontology         │  of-ml-aip            │
   │  edge-gateway         │  connector-mgmt   │  ontology-definition │  model-catalog        │
   │  identity-federation  │  ingestion-repl   │  ontology-actions    │  model-deployment     │
   │  authorization-policy │  dataset-versioni │  ontology-query      │  agent-runtime        │
   │  tenancy-orgs         │  lineage          │  object-database     │  llm-catalog          │
   │                       │  pipeline-build   │  ontology-indexer*   │  retrieval-context    │
   │                       │  sql-bi-gateway   │  outbox-relay*       │  ai-evaluation        │
   │                       │                   │                      │  ai-sink*             │
   └───────────────────────┴───────────────────┴──────────────────────┴───────────────────────┘
                                     │
                        ┌────────────┴────────────────────────────────────┐
                        │  of-apps-ops                                    │
                        │  application-composition  notebook-runtime      │
                        │  ontology-exploratory     solution-design       │
                        │  workflow-automation      notification-alerting │
                        │  audit-compliance + audit-sink*                 │
                        │  telemetry-governance                           │
                        │  federation-product-exchange                    │
                        │  code-repository-review   sdk-generation        │
                        │  entity-resolution                              │
                        └─────────────────────────────────────────────────┘
                                     │
   ┌─────────┬───────────┬──────────┬┴────────┬─────────┬───────────┬─────────────┐
   │ Cassandra│ Postgres │  Kafka   │ Iceberg │ Vespa   │ Temporal  │ Ceph (S3)   │
   │ (S7 mre) │ (CNPG +  │ (Strimzi │ (Lake-  │ (search │ (workflow │ (multisite, │
   │          │  PgBoun) │  + MM2)  │  keeper)│  + RAG) │  engine)  │  S7)        │
   └──────────┴──────────┴──────────┴─────────┴─────────┴───────────┴─────────────┘

   * = Kafka sinks (out of the 30-service count).
```

Recommended entry points:

- [`docs/index.md`](docs/index.md) for the technical documentation homepage
- [`docs/guide/repository-map.md`](docs/guide/repository-map.md) for the monorepo layout
- [`docs/architecture/index.md`](docs/architecture/index.md) for the system overview
- [`docs/operations/ci-cd.md`](docs/operations/ci-cd.md) for delivery and automation flows

At a high level, OpenFoundry is a platform monorepo composed of:

- a SvelteKit frontend in `apps/web`
- a Rust gateway plus multiple bounded-context services in `services/`
- shared Rust foundations in `libs/`
- protobuf contracts in `proto/`
- generated SDK and schema artifacts in `sdks/` and `apps/web/static/generated/`
- infrastructure packaging and runbooks in `infra/`

## Ontology

The Ontology bounded context is split across several Rust services that share
the `libs/ontology-kernel` core. The most relevant runtime detail for the
Action Types surface (TASKs E – Q) lives in
[`services/ontology-actions-service/README.md`](services/ontology-actions-service/README.md)
and is summarised in
[`docs/architecture/capability-map.md`](docs/architecture/capability-map.md#ontology-actions-service--runtime-detail).

`ontology-actions-service` (binary on TCP `50106`) depends on:

- **Postgres** for `action_types`, `action_executions` (revert ledger) and
  `action_what_if_branches`.
- **`audit-compliance-service`** for execution audit trails.
- **`notification-alerting-service`** for action notifications (≤ 500 / ≤ 50
  recipient caps from `Scale and property limits.md`).
- **`connector-management-service`** for webhook writeback / side-effects.
- **`object-database-service`** for the object-instance write path.
- **`ontology-definition-service`** for object-type / property schema lookups.
