# ADR-0007: Search engine choice — Vespa only (no OpenSearch)

- **Status:** Accepted
- **Date:** 2026-04-29
- **Deciders:** OpenFoundry platform architecture group
- **Supersedes:** the search-stack portion of the original data-plane proposal
  that bundled OpenSearch alongside Vespa, plus the planned
  `libs/search-index` crate with `meilisearch` and `opensearch` backends.
- **Related work:** Vespa adoption for vector + lexical retrieval, pgvector for
  embedded vector search, Meilisearch in `infra/docker-compose.yml` (DX only).

## Context

The initial data-plane proposal called for two stateful search clusters in
production:

- **OpenSearch** — BM25 lexical search, log/observability indexing,
  Kibana-style dashboards, classic operational search use cases.
- **Vespa** — vector retrieval and hybrid ranking for AI / semantic features.

A shared abstraction crate (`libs/search-index`) was meant to multiplex
queries across both backends, with an additional `meilisearch` backend for
local DX.

After a focused review of Vespa's capabilities, the operational cost of
running OpenSearch in parallel is no longer justified for OpenFoundry's
target workloads:

- Vespa natively supports **BM25**, **dense vector** (HNSW / ANN), **hybrid
  retrieval**, **structured filters**, **ranking expressions**, **streaming
  search**, **grouping/aggregation**, and **tensor-based reranking** within a
  single cluster.
- Vespa is Apache-2.0 licensed (compatible with the project's OSS posture
  after Qdrant was retired for the same reason — see also
  `docs/operations/deployment.md`).
- pgvector already covers small / embedded vector use cases that live next
  to relational state, removing pressure to push every embedding into a
  search cluster.
- Most of the operational dashboards we need (logs, traces, metrics) are
  served by the existing **observability stack** (Prometheus, Loki, Tempo,
  Grafana) — not by a Kibana-style UI on top of OpenSearch.

Running both OpenSearch and Vespa would duplicate stateful infrastructure
(JVM heaps, snapshot/restore tooling, version upgrades, capacity planning,
on-call surface, security hardening) for capabilities that overlap
substantially.

## Options considered

### Option A — Vespa only + pgvector (chosen)

- One stateful search cluster (Vespa) for BM25, vector, hybrid and ranking.
- pgvector for service-local embedded vector search co-located with
  relational data.
- Meilisearch retained **only** in `infra/docker-compose.yml` for local
  developer experience (fast first-run, zero-JVM, instant search demos).

**Pros**

- Single search cluster to operate, scale, secure, back up and upgrade.
- Hybrid (BM25 + vector + filters + ranking) in one query plan.
- Apache-2.0; no licensing risk.
- Reduces cognitive load for service authors: one query API for production
  search, one (Postgres) for embedded vector lookups.
- Frees the roadmap from designing/maintaining the `libs/search-index`
  multi-backend abstraction.

**Cons**

- No Kibana-equivalent out of the box for ad-hoc log exploration over very
  large corpora.
- Vespa has a steeper learning curve than OpenSearch for teams already
  familiar with the Elastic ecosystem.

### Option B — Vespa + OpenSearch (original proposal)

- OpenSearch for lexical / log search and dashboards.
- Vespa for vector and hybrid AI retrieval.
- `libs/search-index` crate to abstract both backends behind one API.

**Pros**

- Familiar Elastic-style query DSL and Kibana-style dashboards available.
- Clear separation between "operational" and "AI" search.

**Cons**

- Two stateful clusters to operate, secure and scale.
- Duplicated capabilities (BM25, filters, aggregations) across backends.
- The abstraction crate becomes a long-lived liability: every backend
  feature (highlighting, faceting, ranking expressions, snapshots) has to
  be reconciled across two very different engines.
- Higher footprint for air-gapped / single-node deployments.

### Option C — OpenSearch only

- One cluster for both lexical and vector use cases.

**Pros**

- One stack to operate.
- Mature ecosystem, dashboards, integrations.

**Cons**

- Vector / hybrid story (k-NN plugin) is less mature than Vespa's tensor
  ranking and streaming search for the AI workloads we expect to run in
  the AIP capabilities.
- Ranking expressiveness is lower than Vespa's, which we rely on for
  ontology-aware retrieval and reranking.
- Same licensing/governance review burden as adopting Vespa, without
  Vespa's retrieval ceiling.

## Decision

We adopt **Option A — Vespa only + pgvector**.

- **Production search and vector retrieval** run on a single Vespa cluster.
  All hybrid (BM25 + vector + filter + rank) queries that the platform
  exposes go through Vespa.
- **Embedded / co-located vector search** (small corpora living next to
  relational state, e.g. service-local similarity lookups) uses **pgvector**
  on the existing PostgreSQL instances.
- **Meilisearch is kept only in `infra/docker-compose.yml`** for local
  developer experience. It is **not** a production component, has no Helm
  chart, and is not exposed by Argo CD. Service code that targets
  production must not depend on Meilisearch behaviour beyond what Vespa
  also provides.
- **OpenSearch is not deployed.** No Helm chart, no Argo CD `Application`,
  no Terraform module, no operator manifest will be added for OpenSearch
  while this ADR stands.
- The previously planned **`libs/search-index` crate with `meilisearch`
  and `opensearch` backends is dropped**. Services should depend on a
  Vespa client (or pgvector, where appropriate) directly, without an
  abstraction layer designed to hide multiple production backends.

## Consequences

### Positive

- One stateful search cluster to operate end-to-end (capacity, upgrades,
  backup/restore, security, on-call).
- Hybrid retrieval (lexical + vector + filters + ranking) lives in one
  query plan, removing client-side fan-out and re-ranking glue.
- Smaller attack and dependency surface; no JVM-on-JVM duplication for
  capabilities that overlap.
- Smaller footprint for air-gapped / single-node deployments.
- Documentation, runbooks and SDK guidance can converge on a single
  production search story.

### Negative / trade-offs

- No turn-key Kibana-equivalent for ad-hoc exploration of very large log
  corpora. Operational log/trace/metric exploration continues to rely on
  the existing observability stack (Loki / Tempo / Prometheus / Grafana).
- Teams with prior Elastic experience need ramp-up time on Vespa schemas,
  ranking expressions and deployment packages.
- Service authors lose the (theoretical) freedom to swap search backends
  via `libs/search-index`; choosing Vespa is a load-bearing platform
  commitment.

### Migration / cleanup

- No production OpenSearch manifests existed at the time of this decision —
  there is nothing to remove from `infra/k8s/helm/**` or from any Argo CD
  `Application`.
- Any future task description, roadmap entry or design note that mentions
  "OpenSearch" as a planned component must instead link to this ADR and
  state that the capability is provided by Vespa (or pgvector for embedded
  cases).
- Meilisearch usage is restricted to `infra/docker-compose.yml` and to
  documentation describing local development. It must not appear in Helm
  values, Argo CD applications, or production runbooks.

## Conditions under which this decision would be reopened

This ADR should be revisited if **any** of the following becomes true:

1. **Kibana-style dashboards for non-AI domains** become a hard product
   requirement on a corpus larger than roughly **1 TB** of indexed data,
   and the existing observability stack (Loki / Grafana) cannot satisfy
   it.
2. Vespa's licensing, governance or release cadence changes in a way that
   makes it unsuitable for our OSS posture (cf. the Qdrant precedent).
3. A concrete workload demonstrates that Vespa's hybrid ranking, ANN
   recall or operational ceiling is materially worse than OpenSearch's
   k-NN / ranking story for OpenFoundry-shaped queries, with reproducible
   benchmarks under `benchmarks/`.
4. A regulated deployment target mandates OpenSearch (or an
   Elastic-derived engine) as the only certified search component.

If any of the above triggers fires, the follow-up ADR must explicitly
re-evaluate (a) whether OpenSearch is added **alongside** Vespa or
**replaces** part of it, and (b) whether the `libs/search-index`
abstraction needs to be reintroduced.

## References

- `infra/docker-compose.yml` — Vespa Lite single-node container (DX) and
  the canonical reference to the production search engine.
- `infra/docker-compose.dev.yml` — Meilisearch under the optional
  `demo` profile (first-run demo only).
- `docs/architecture/runtime-topology.md` — shared runtime dependencies.
- `docs/operations/deployment.md` — local stack and Kubernetes packaging.
- Vespa documentation: <https://docs.vespa.ai/>
- pgvector: <https://github.com/pgvector/pgvector>

## Addendum — 2026-04: consolidación final

> **Estado:** Aceptado · **Fecha:** 2026-04-29

Tras validar que ningún servicio del workspace, ningún test de
integración y ningún escenario en `smoke/` consume Meilisearch (solo
quedaba la declaración no usada `meilisearch-sdk` en
`Cargo.toml [workspace.dependencies]`), se consolida la decisión de la
siguiente forma:

- **Vespa Lite** (`vespaengine/vespa`, Apache-2.0) pasa a ser la
  dependencia de búsqueda por defecto para **DX local**, expuesta como
  un único contenedor single-node en `infra/docker-compose.yml`. Es el
  mismo motor que el de producción descrito en `infra/runbooks/vespa.md`
  e `infra/k8s/platform/charts/vespa/`, lo que elimina la
  divergencia DX↔producción que motivaba mantener Meilisearch.
- **Meilisearch** se traslada a `infra/docker-compose.dev.yml` bajo el
  perfil opcional `--profile demo`. Solo se levanta cuando se quiere
  reproducir el "first-run demo" (búsqueda instantánea sin JVM); no es
  una dependencia ni de DX común ni de producción.
- Los scripts (`infra/scripts/dev-stack.sh`), el `justfile` y
  `.env.example` reservan/exportan puertos y endpoints para Vespa por
  defecto; las variables `MEILISEARCH_URL` / `OPENFOUNDRY_MEILISEARCH_HOST_PORT`
  solo se materializan si el operador pide explícitamente el perfil
  `demo`.
- Ninguna de las condiciones de reapertura listadas más arriba se ha
  disparado: Vespa sigue siendo Apache-2.0, no se ha materializado un
  requisito de Kibana sobre >1 TB, no hay benchmarks que demuestren
  inferioridad de Vespa para nuestras cargas y no hay imposición
  regulatoria de OpenSearch.

Cambios concretos asociados a este addendum (2026-04):

- `infra/docker-compose.yml`: se elimina el servicio `meilisearch` y su
  volumen `meilisearch_data`, y se añade el servicio `vespa` single-node
  con healthcheck contra `:19071/state/v1/health` y volumen `vespa_data`.
- `infra/docker-compose.dev.yml`: se añade `meilisearch` con
  `profiles: ["demo"]` y volumen `meilisearch_data`, exclusivamente para
  el demo opcional.
- Documentación (`docs/operations/deployment.md`,
  `docs/guide/local-development.md`,
  `docs/architecture/runtime-topology.md`) actualizada para retirar
  Meilisearch de la lista de dependencias comunes y apuntar al perfil
  `demo`.

Si en el futuro se decidiera retirar también el demo, la declaración
`meilisearch-sdk` en `Cargo.toml` puede eliminarse junto con este
servicio sin afectar a ningún consumidor.
