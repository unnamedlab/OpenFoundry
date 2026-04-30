# ADR-0008: Iceberg REST Catalog — Lakekeeper only

- **Status:** Accepted
- **Date:** 2026-04-29
- **Deciders:** OpenFoundry platform architecture group
- **Supersedes:** the open-ended catalog mention in
  `libs/storage-abstraction/README.md`
  ("any Iceberg REST Catalog (Polaris, Lakekeeper, Nessie, Tabular, …)").
- **Related work:** `iceberg = "0.9"` and `iceberg-catalog-rest = "0.9"`
  integration in the `storage-abstraction` crate (see
  `libs/storage-abstraction/README.md`); CNPG-based PostgreSQL operator
  (see ADR-0010).

## Context

`libs/storage-abstraction` integrates with Apache Iceberg through the
`iceberg = "0.9"` and `iceberg-catalog-rest = "0.9"` crates (both
Apache-2.0). Because the crate talks to an Iceberg **REST Catalog**, the
public README currently advertises that *any* REST-compatible
implementation is acceptable, listing **Polaris, Lakekeeper, Nessie and
Tabular** as interchangeable choices.

In practice, leaving the catalog choice open at the platform level is an
operational risk for OpenFoundry:

- Each candidate has a different deployment model, auth story, multi-tenant
  story, schema evolution semantics and snapshot/maintenance tooling.
  Supporting "any of them" means we cannot ship a single Helm chart, a
  single runbook, a single backup procedure or a single upgrade path.
- The reference `compose.yaml` and the Helm assets under `infra/` need a
  concrete catalog to point at; documenting four equivalent options leaves
  contributors guessing which one OpenFoundry actually exercises in CI and
  in production.
- Some of the listed options are not aligned with our 100% OSS
  (Apache-2.0 / MIT / BSD) posture, or are tightly coupled to a single
  vendor's ecosystem, so they cannot be a safe default.
- Without a single supported catalog, on-call rotations would have to
  carry expertise for several heterogeneous services for a capability
  (catalog metadata) that has very low business differentiation.

We therefore need a single, supported Iceberg REST Catalog implementation
for OpenFoundry, while keeping the `storage-abstraction` crate's generic
API (which only depends on `iceberg-catalog-rest`) untouched so that
integrators that already operate a different REST catalog can still point
the crate at their own endpoint at their own risk.

## Options considered

### Option A — Lakekeeper (chosen)

- Apache-2.0 licensed, written in Rust.
- Multi-tenant by design (warehouses / projects / namespaces).
- Native OIDC integration, which lines up with the rest of the
  OpenFoundry control plane.
- Stateless service; metadata is persisted in **PostgreSQL**, which we
  already operate via CloudNativePG (see ADR-0010), so we do not introduce
  a new stateful system.
- Implements the Iceberg REST Catalog spec that `iceberg-catalog-rest =
  "0.9"` already speaks, so no client-side changes are required in
  `libs/storage-abstraction`.

### Option B — Apache Polaris

- Apache-2.0, but its design and roadmap are tightly coupled to
  **Snowflake**'s ecosystem and operational model.
- Adopting Polaris as the platform default would create a soft dependency
  on a single-vendor trajectory that we do not control.

### Option C — Project Nessie

- Provides Git-like branching/merging semantics over catalog state.
- That semantic is **not required** by any current OpenFoundry workload;
  paying the operational and conceptual cost of branchable metadata
  without a concrete consumer is not justified today.

### Option D — Tabular

- Commercial, managed offering; not a pure-OSS, self-hostable component
  that fits our 100% OSS (Apache-2.0 / MIT / BSD) posture.
- Disqualified as a platform default for the same reason other commercial
  catalogs are not in scope.

## Decision

We adopt **Option A — Lakekeeper** as the single Iceberg REST Catalog
supported by OpenFoundry in production.

- Lakekeeper is deployed as a Kubernetes **`Deployment` with 3 replicas**
  (stateless), fronted by the standard ingress used by the rest of the
  control plane.
- Catalog **metadata is stored in PostgreSQL managed by CloudNativePG**
  (see ADR-0010); no new stateful system is introduced.
- Authentication is delegated to the platform OIDC provider via
  Lakekeeper's native OIDC support.
- `libs/storage-abstraction` keeps its **generic** Iceberg REST client
  (`iceberg-catalog-rest = "0.9"`); the crate's public API is **not**
  changed by this ADR. Only the documentation is tightened to name
  Lakekeeper as the supported catalog.
- Polaris, Nessie and Tabular are **not** deployed by OpenFoundry. No
  Helm chart, Argo CD `Application` or compose service is provided for
  them.

## Consequences

### Positive

- One catalog implementation to operate, scale, secure, back up and
  upgrade.
- A single runbook, a single Helm chart and a single set of dashboards
  for catalog metadata.
- Reuses the existing CNPG-managed PostgreSQL footprint instead of
  introducing yet another stateful system.
- Native OIDC keeps catalog auth aligned with the rest of the platform.
- Documentation, examples and `compose.yaml` can converge on one concrete
  endpoint instead of four equivalent ones.

### Negative / trade-offs

- Integrators that already standardised on Polaris, Nessie or Tabular
  must either keep operating those themselves (the generic REST client in
  `storage-abstraction` still works against them) or migrate to
  Lakekeeper to get full platform support.
- Choosing Lakekeeper is a load-bearing platform commitment: tying our
  Helm chart, runbooks and CI to one implementation means a future
  re-evaluation has a non-trivial migration cost.

### Migration plan

1. Update `libs/storage-abstraction/README.md` to mention **only
   Lakekeeper** as the catalog supported by OpenFoundry in production
   (link this ADR), while keeping the crate's generic API intact so the
   underlying `iceberg-catalog-rest = "0.9"` client is unchanged.
2. Subsequent platform work (out of scope for this ADR) updates
   `compose.yaml` and the Helm assets under `infra/` to ship Lakekeeper
   as the single catalog service, backed by CNPG-managed PostgreSQL per
   ADR-0010.
3. Any future task description, roadmap entry or design note that
   mentions Polaris, Nessie or Tabular as a planned catalog component
   must instead link to this ADR and state that the supported catalog is
   Lakekeeper.

## References

- `libs/storage-abstraction/README.md` — Iceberg integration and the
  previous open-ended catalog mention this ADR supersedes.
- ADR-0007 (`docs/architecture/adr/ADR-0007-search-engine-choice.md`) —
  precedent for picking a single OSS implementation over a multi-backend
  abstraction.
- Lakekeeper: <https://github.com/lakekeeper/lakekeeper> (Apache-2.0).
- Apache Iceberg REST Catalog spec: <https://iceberg.apache.org/>
