# OpenFoundry Architecture

The canonical technical documentation for this repository now lives in [`docs/`](docs/).

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
