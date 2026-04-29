# ADR-0011: Control vs Data bus — contract enforcement

- **Status:** Accepted
- **Date:** 2026-04-29
- **Deciders:** OpenFoundry platform architecture group
- **Related work:**
  - `libs/event-bus-control` (NATS JetStream)
  - `libs/event-bus-data` (Apache Kafka)
  - [`docs/architecture/runtime-topology.md` §"Control Plane vs Data Plane (Event Bus split)"](../runtime-topology.md)

## Context

The repository already separates the two event-bus crates by transport and
intent:

- `libs/event-bus-control` → NATS JetStream, control-plane traffic
  (latency-sensitive signals, RPC-ish events, fan-out, workflow triggers).
- `libs/event-bus-data` → Apache Kafka, data-plane firehoses
  (CDC streams, ingestion, lineage events, audit archive).

This split is documented in
[`docs/architecture/runtime-topology.md`](../runtime-topology.md) §"Control
Plane vs Data Plane (Event Bus split)" (see the table at lines 72–75 of that
file).

However, the separation today is **only a convention**. Nothing in the build
prevents a service from:

- depending on `event-bus-data` (Kafka) to push small, latency-sensitive
  control signals — abusing Kafka as a control queue, or
- depending on `event-bus-control` (NATS JetStream) to publish multi-GB/s
  data firehoses — overloading the control plane.

Either drift would erode the operational guarantees described in
`runtime-topology.md` §"Why two buses" (control-plane outages must not block
data ingestion; runaway data producers must not starve control signals).

## Decision

We make the control-vs-data split a **contractual, mechanically-checked
property** of every service crate.

1. **Contract rule.** The Cargo dependencies of each service on
   `event-bus-control` and/or `event-bus-data` must respect the matrix
   documented in `docs/architecture/runtime-topology.md` §"Control vs Data".
   Each service explicitly declares which bus(es) it is allowed to use.

2. **Allowlist file.** The current authoritative matrix lives in
   `/.github/bus-allowlist.yaml`. It maps each service crate name to the
   list of buses it may depend on (`control`, `data`, or both). The file
   starts as a **snapshot of the current state** of the repository (no
   service is migrated by this ADR).

3. **CI check.** A small Python 3 script (no extra dependencies) lives at
   `/tools/bus-lint/check_bus.py`. It:

   - walks `/services/*/Cargo.toml`,
   - parses each `Cargo.toml` (via the standard-library `tomllib`),
   - determines whether the service depends on `event-bus-control` and/or
     `event-bus-data`,
   - cross-references the result with `/.github/bus-allowlist.yaml`,
   - **fails CI** if any service:
     - depends on a bus that is not declared for it in the allowlist,
     - is missing from the allowlist while depending on either bus,
     - is listed in the allowlist for a bus it no longer uses (stale
       entry).

4. **Workflow integration.** The check is wired into the existing
   `.github/workflows/ci.yml` workflow as a new lightweight job
   (`bus-contract`) that runs on every `pull_request`. We deliberately
   reuse the existing Rust CI workflow rather than introducing a new
   workflow file, keeping the lint surface narrow and discoverable.

## Consequences

- **Positive.** No service can silently "smuggle" Kafka into the control
  plane (or NATS into the data plane). Any such change requires a PR that
  also updates `/.github/bus-allowlist.yaml`, which forces architectural
  review by the owners of `runtime-topology.md`.
- **Positive.** The allowlist becomes a living, machine-checked index of
  the platform's bus topology, complementing the prose in
  `runtime-topology.md`.
- **Neutral.** Adding a legitimate new bus dependency now requires a
  one-line allowlist update in the same PR — a deliberate, low-cost
  speed bump.
- **Negative.** The script duplicates a tiny amount of TOML/YAML parsing
  logic; we accept this in exchange for keeping the dependency surface
  at zero (only the Python 3 standard library is used).

## Notes

- The allowlist is authoritative for **services only**
  (`/services/*/Cargo.toml`). Library crates under `/libs/**` are out of
  scope — they are the producers of the bus abstractions, not consumers.
- This ADR does not change any service code. It only adds documentation,
  the allowlist, the lint script, and a CI job.
