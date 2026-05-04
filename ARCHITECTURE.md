# OpenFoundry Architecture

The canonical technical documentation for this repository now lives in [`docs/`](docs/).

## Post-S8 service pyramid (95 service directories, 33 ownership boundaries + 3 sinks)

Stream S8 (cleanup & hardening) tracks consolidation as ownership and
release alignment, not as a claim that the source tree has been physically
reduced to 30 directories. The live repository has 95 service directories;
the current target metric is 33 ownership boundaries plus 3 Kafka sinks,
packaged across five Helm releases. See
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
   │                       │  media-sets       │  ontology-indexer*   │  retrieval-context    │
   │                       │  pipeline-build   │                      │  ai-evaluation        │
   │                       │  sql-bi-gateway   │                      │                       │
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

   * = Kafka sinks (counted separately from ownership boundaries).
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

## Media sets

Foundry-style **media sets** are the system of record for unstructured
files (image, audio, video, document, spreadsheet, email). The bounded
context is owned by [`services/media-sets-service`](services/media-sets-service/README.md);
the H3 closure stitches it into the platform's authz, audit and
observability planes.

The flow below is the canonical reference — every neighbour service
exists in this repo today, and every dotted edge has a wire-format
contract pinned by an ADR (linked in the per-service README).

```mermaid
flowchart LR
  subgraph DataPlane["Data plane"]
    direction LR
    Source[("External source\n(S3 / ABFS / …)")]
    Connector["connector-management-service\n(media_set_syncs)"]
    Media["media-sets-service\n(media_sets / media_items / transactions)"]
    Pipeline["pipeline-authoring-service\n(MediaSetInput → MediaTransform →\nMediaSetOutput)"]
    Dataset[("data-asset-catalog-service\n(derived dataset)")]
    Ontology["ontology-actions-service\n(IsValidMediaReference,\nConstructDelegatedMediaGid)"]
    Workshop[("apps/web Workshop\nMedia preview widget")]

    Source -->|enumerate + classify| Connector
    Connector -->|POST /upload-url\nor /virtual-items| Media
    Media -->|presigned download| Pipeline
    Pipeline -->|writeback / dataset rows| Dataset
    Pipeline -->|media references| Ontology
    Ontology -->|delegatedMediaGid| Workshop
    Media -->|presigned read\n(JWT-signed claim, H3)| Workshop
  end

  subgraph AuthzPlane["Authz plane (H3, ADR-0027)"]
    Cedar["authz-cedar engine\n(MediaSet / MediaItem entities,\n6 actions · granular markings)"]
  end

  subgraph AuditPlane["Audit plane (H3 closure, ADR-0022)"]
    Outbox["pg-policy.outbox.events\n(per-handler INSERT in same tx)"]
    Debezium["Strimzi Debezium\nKafkaConnect cluster"]
    Topic[/"audit.events.v1"/]
    Sink["audit-sink\n(Iceberg of_audit.events)"]
    Outbox --> Debezium --> Topic --> Sink
  end

  Connector -.gate.-> Cedar
  Media -.gate.-> Cedar
  Pipeline -.gate.-> Cedar
  Ontology -.gate.-> Cedar

  Connector -.audit.-> Outbox
  Media ==>|emit per mutation| Outbox
  Pipeline -.audit.-> Outbox
  Ontology -.audit.-> Outbox

  classDef ours fill:#0f766e,stroke:#0f766e,color:#fff;
  class Media ours;
```

Per-plane contracts:

- **Data plane.** `connector-management-service` decides whether an
  inbound file becomes a regular media item (bytes copied to Foundry
  storage via a presigned PUT) or a virtual item (metadata only;
  bytes stay in the source system). The downstream pipeline
  consumes / produces media items via the
  [Pipeline Builder media nodes](services/pipeline-authoring-service/README.md).
- **Authz plane.** Every read / write goes through the embedded
  Cedar engine ([`libs/authz-cedar`](libs/authz-cedar/README.md)). The
  H3 schema adds `MediaSet` and `MediaItem` entities, six actions,
  and the granular per-item marking rule that lets an operator
  tighten access on a single sensitive image without locking the
  whole set. Presigned URLs carry a 5-minute HMAC claim the edge
  gateway re-validates (no clearance ⇒ no URL).
- **Audit plane.** Per ADR-0022 every mutation handler in
  `media-sets-service` enqueues an
  [`audit_trail::events::AuditEvent`](libs/audit-trail/src/events.rs)
  envelope into `pg-policy.outbox.events` inside the same SQL
  transaction as the primary write. Strimzi Debezium drains the
  WAL to `audit.events.v1`; `audit-sink` materialises the topic
  into the `of_audit.events` Iceberg table. SIEM queries filter on
  the Foundry-style `categories` field
  (`dataCreate / dataExport / managementMarkings / …`) so new event
  variants slot into existing alerts without rule churn.

Cost-model metrics specific to media live in
[`services/media-sets-service/src/metrics.rs`](services/media-sets-service/src/metrics.rs)
and the alerts that fire off them are in
[`infra/k8s/platform/observability/prometheus-rules/media-sets.yaml`](infra/k8s/platform/observability/prometheus-rules/media-sets.yaml)
(storage-budget breach, retention-purge anomaly, stuck transaction).
