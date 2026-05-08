# Audit and Reference No-SPOF

This page is the stable documentation anchor for older ADRs that cite the platform audit of single points of failure, persistence topology, and fit-for-purpose runtime stores.

## Current Reference Set

The original audit findings are now carried by the follow-up ADRs and architecture notes below:

- [ADR-0011 — Control vs data bus contract](./adr/ADR-0011-control-vs-data-bus-contract.md) defines the control-plane vs data-plane messaging split.
- [ADR-0020 — Cassandra as operational store](./adr/ADR-0020-cassandra-as-operational-store.md) documents why write-heavy, TTL-native, multi-DC operational state moved away from per-service single-leader Postgres.
- [ADR-0024 — Postgres consolidation](./adr/ADR-0024-postgres-consolidation.md) documents the consolidation of many service-local CNPG clusters into purpose-scoped Postgres clusters.
- [Runtime topology](./runtime-topology.md) tracks the current control/data-plane placement.
- [Bus audit](./bus-audit.md) captures the event-bus usage review that validates the ADR-0011 split.

Keep links to this page when an ADR needs the historical audit anchor, and update the ADR-specific pages when the implementation details change.
