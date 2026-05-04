# Architecture center

This section groups the repository-wide architecture pages that describe how OpenFoundry works under the hood.

## Included areas

- [Monorepo structure](/architecture/monorepo)
- [Runtime topology](/architecture/runtime-topology)
- [Services and ports](/architecture/services-and-ports)
- [Contracts and SDKs](/architecture/contracts-and-sdks)
- [Capability map](/architecture/capability-map)

## Architecture Decision Records

- [ADR-0007 — Search engine choice (Vespa only, no OpenSearch)](/architecture/adr/ADR-0007-search-engine-choice)
- [ADR-0037 — Foundry-pattern orchestration](/architecture/adr/ADR-0037-foundry-pattern-orchestration)
- [ADR-0038 — Event contract and idempotency for Foundry-pattern orchestration](/architecture/adr/ADR-0038-event-contract-and-idempotency)

## Focus

The pages in this section explain:

- how the monorepo is partitioned
- which services own which parts of the runtime
- how contracts become generated SDKs and UI-facing schemas
- how critical capability chains show up in smoke scenarios
