# Architecture Decision Records (ADRs)

This directory holds the OpenFoundry Architecture Decision Records. Each ADR
documents a single, scoped decision: the context that forced it, the options
that were considered, the choice that was made, and the consequences that
follow.

## Index

| ADR file                                                                                                                                  | Status   | Date       | Title                                                                                  |
| ----------------------------------------------------------------------------------------------------------------------------------------- | -------- | ---------- | -------------------------------------------------------------------------------------- |
| [`ADR-0007-search-engine-choice.md`](./ADR-0007-search-engine-choice.md)                                                                  | Accepted | 2026-04-29 | Search engine choice — Vespa only (no OpenSearch)                                      |
| [`ADR-0008-iceberg-rest-catalog-lakekeeper.md`](./ADR-0008-iceberg-rest-catalog-lakekeeper.md)                                            | Accepted | 2026-04-29 | Iceberg REST Catalog — Lakekeeper only                                                 |
| [`ADR-0009-internal-query-fabric-datafusion-flightsql.md`](./ADR-0009-internal-query-fabric-datafusion-flightsql.md)                      | Accepted | 2026-04-29 | Internal query fabric — DataFusion + Flight SQL (Trino as edge BI only)                |
| [`ADR-0010-cnpg-postgres-operator.md`](./ADR-0010-cnpg-postgres-operator.md)                                                              | Accepted | 2026-04-29 | CloudNativePG (CNPG) as the single PostgreSQL operator                                 |
| [`ADR-0011-control-vs-data-bus-contract.md`](./ADR-0011-control-vs-data-bus-contract.md)                                                  | Accepted | 2026-04-29 | Control vs Data bus — contract enforcement (NATS JetStream vs Kafka)                   |
| [`ADR-0012-data-plane-slos.md`](./ADR-0012-data-plane-slos.md)                                                                            | Accepted | 2026-04-29 | Data-plane SLOs, SLIs and error budgets                                                |
| [`ADR-0013-kafka-kraft-no-spof-policy.md`](./ADR-0013-kafka-kraft-no-spof-policy.md)                                                      | Accepted | 2026-04-30 | Kafka KRaft no-SPOF policy and upgrade procedure                                       |

ADR-0001 through ADR-0006 are historical placeholders and are intentionally
not present in this repository: the data-plane consolidation effort that
introduced this directory only ratified ADR numbers from `0007` onwards.
Future ADRs **MUST** continue the sequence starting at `ADR-0013` — do **not**
back-fill the `0001`–`0006` slots.

## ROADMAP plan-item ↔ ADR mapping (audit aid)

The "data-plane consolidation" plan in
[`ROADMAP.md`](../../../ROADMAP.md) (the bullet list under
*"Foundational data-plane consolidation"*) numbers the ADR-bearing items
sequentially `1`..`5`, while the **filenames on disk are
`ADR-0008`..`ADR-0012`** — they are not the same number. The corollary
ADR-0007 (search engine) is referenced by plan item **15**, not by the
opening ADR group. This table exists so that future audits can resolve any
"plan said ADR 8/9/10/11/12, why does the file system look different?"
confusion in a single lookup.

| ROADMAP plan item                            | ADR file (on disk)                                                                                                   | Subject                                                              |
| -------------------------------------------- | -------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------- |
| **1.** ADR-0008 — Iceberg REST catalog       | [`ADR-0008-iceberg-rest-catalog-lakekeeper.md`](./ADR-0008-iceberg-rest-catalog-lakekeeper.md)                       | Single Iceberg REST catalog (Lakekeeper).                            |
| **2.** ADR-0009 — Internal query fabric      | [`ADR-0009-internal-query-fabric-datafusion-flightsql.md`](./ADR-0009-internal-query-fabric-datafusion-flightsql.md) | DataFusion + Flight SQL P2P; Trino is edge-BI only.                  |
| **3.** ADR-0010 — Postgres operator          | [`ADR-0010-cnpg-postgres-operator.md`](./ADR-0010-cnpg-postgres-operator.md)                                         | CloudNativePG as the single Postgres operator.                       |
| **4.** ADR-0011 — Bus contract               | [`ADR-0011-control-vs-data-bus-contract.md`](./ADR-0011-control-vs-data-bus-contract.md)                             | Control (NATS) vs Data (Kafka) bus; CI-enforced contract.            |
| **5.** ADR-0012 — Data-plane SLOs            | [`ADR-0012-data-plane-slos.md`](./ADR-0012-data-plane-slos.md)                                                       | Per-layer latency SLOs/SLIs, error budgets, freeze policy.           |
| **15.** ADR-0007 consolidation               | [`ADR-0007-search-engine-choice.md`](./ADR-0007-search-engine-choice.md)                                             | Vespa-only search; Vespa Lite for DX; Meilisearch demoted.           |

### Why the mismatch exists

ADR-0007 was ratified earlier — as a standalone search-stack decision — and
keeps its original number for backwards compatibility with all the existing
cross-references in code, runbooks and the
[architecture-center index](../../architecture-center/index.md). The five
ADRs that came out of the data-plane consolidation effort were then
allocated the next free numbers (`0008`..`0012`) in chronological order of
authorship, **not** in the order they appear in the plan list. The plan
list, in turn, was numbered `1`..`5` for human readability.

This mapping is the canonical reconciliation; if any documentation
disagrees, this README wins.

## Adding a new ADR

1. Pick the next free four-digit number (currently **`ADR-0014`**) — never
   reuse a previous number even if the ADR was retracted.
2. Create `ADR-NNNN-short-kebab-title.md` in this directory.
3. Use the standard heading layout already in place across `ADR-0007` …
   `ADR-0012`:

   - `# ADR-NNNN: <Title>`
   - `- **Status:**` *(`Proposed` / `Accepted` / `Superseded by ADR-NNNN` /
     `Deprecated`)*
   - `- **Date:**` ISO-8601
   - `- **Deciders:**`
   - `- **Supersedes:**` *(optional — link to the prior decision)*
   - `- **Related work:**` *(optional — links to crates / manifests / runbooks)*
   - `## Context` → `## Decision` → `## Consequences` (and optional
     `## Migration plan`, `## Alternatives considered`).

4. Add the new entry to **both** tables in this README (the *Index* and the
   *plan ↔ ADR mapping* if it belongs to a roadmap effort) and link it from
   [`docs/architecture-center/index.md`](../../architecture-center/index.md).
