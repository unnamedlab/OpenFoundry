# ADR-0046: Park streaming-profile CRUD inside `identity-federation-service` control-panel

- **Status:** Proposed
- **Date:** 2026-05-17
- **Deciders:** Streaming + Frontend WG (pending review)
- **Related ADRs:**
  - [ADR-0035](./ADR-0035-streams-foundry-parity.md) — Streams Foundry parity. P3 ("Streaming profiles + project refs", migration `20260504000003_streaming_profiles.sql`) targeted `ingestion-replication-service` but never landed on the Go side after the Rust → Go cleanup.
- **Related code:**
  - [`services/identity-federation-service/internal/handlers/control_panel.go`](../../../services/identity-federation-service/internal/handlers/control_panel.go) — sibling in-memory resources (`FileAccessPresetConfig`, `ApplicationAccessConfig`, `ScopedSessionConfig`, `MemberDiscoveryConfig`).
  - [`services/ingestion-replication-service/internal/server/server.go`](../../../services/ingestion-replication-service/internal/server/server.go) — `StreamingMetadata` slot pattern (Schemas / Branches) that the future P3 implementation will fit into.
  - [`apps/web/src/routes/control-panel/StreamingProfilesPage.tsx`](../../../apps/web/src/routes/control-panel/StreamingProfilesPage.tsx) — UI surface for the resource.
  - [`docs/frontend-interaction-matrix.json`](../../../docs/frontend-interaction-matrix.json) — declared but unimplemented endpoints for CTRL-002.

## Context

The frontend ships a placeholder page (`StreamingProfilesPage`) and the
interaction matrix declares `GET/POST /control-panel/streaming-profiles`
plus `PATCH /control-panel/streaming-profiles/:id` as `status: current`,
but no backend handler implements those routes today. The only place in
the repo that names the resource is
[`proto/pipeline/pipeline.proto`](../../../proto/pipeline/pipeline.proto)
(`StreamingConfig.streaming_profile_id`, a consumer field). ADR-0035
foresaw streaming profiles as block P3 inside the streaming control
plane (`ingestion-replication-service`), with a dedicated Postgres
migration; that work was scheduled for the Rust era and did not make
it across the port.

Two delivery paths are viable:

1. **Foundry-parity-correct path.** Implement P3 in
   `ingestion-replication-service`: new migration, new domain package
   (mirroring `streambranch`), repo + handler subpackage,
   integration test. URL: `/api/v1/streaming-profiles`. Lets the
   service surface `last_event_at` / `throughput_eps` directly from
   the same database that holds stream telemetry. Larger slice.
2. **Parking path (this ADR).** Park CRUD in
   `identity-federation-service` control-panel as an in-memory list,
   mirroring `FileAccessPreset` / `ApplicationAccess` /
   `ScopedSessions`. URL: `/api/v1/control-panel/streaming-profiles`.
   Smaller, ships now, unblocks UI. Cost: when P3 lands it will be
   moved out, and the URL prefix will become a breaking change unless
   we plan a redirect.

The streaming WG has explicit priorities ahead of P3 (consistency
matrix, dead-letter retention) and the UI does not currently consume
`last_event_at` / `throughput_eps` — those are best-effort labels.
Postponing P3 by parking CRUD configuration alongside the other
admin-tier control-panel resources is acceptable, provided the
relocation is foreseen.

## Decision

We **park** streaming-profile CRUD inside
`identity-federation-service`'s control-panel handler. Concretely:

- The resource lives as an in-memory list on `ControlPanel` (the
  process-local singleton initialised by `NewControlPanel()`), wire
  shape mirroring the Foundry "streaming profile" concept:

  ```
  StreamingProfile {
    id, name, description?, connector_type, status,            // identity + lifecycle
    parallelism, watermark_policy, checkpoint_interval_ms,     // runtime knobs
    source_config (json), destination_dataset_id?,             // wiring
    last_event_at?, throughput_eps?,                           // optional telemetry
    created_by?, created_at?, updated_by?, updated_at?         // audit
  }
  ```

- Routes (mounted under the existing `/api/v1` bearer-protected
  group, alongside the other control-panel handlers):

  ```
  GET    /api/v1/control-panel/streaming-profiles
  POST   /api/v1/control-panel/streaming-profiles
  GET    /api/v1/control-panel/streaming-profiles/{id}
  PATCH  /api/v1/control-panel/streaming-profiles/{id}
  DELETE /api/v1/control-panel/streaming-profiles/{id}
  POST   /api/v1/control-panel/streaming-profiles/{id}:pause
  POST   /api/v1/control-panel/streaming-profiles/{id}:resume
  ```

- Permissions reuse the existing `requireControlPanelRead` /
  `requireControlPanelWrite` helpers — same model as
  `FileAccessPreset`. No new Cedar policy.

- `connector_type` is validated against a static allowlist that
  mirrors the streaming-source catalogue exposed by
  `connector-management-service` at `/api/v1/data-connection/streaming-sources`
  (`streaming_kafka`, `streaming_kinesis`, `streaming_sqs`,
  `streaming_pubsub`, `streaming_aveva_pi`, `streaming_external`).
  The allowlist is a manual-sync point; if the catalogue grows the
  list must be updated here.

- `last_event_at` and `throughput_eps` are accepted on POST/PATCH
  but **never computed** in this service. A future job
  (out-of-scope) may populate them from telemetry. Until then they
  remain whatever the admin wrote, or nil.

## Consequences

### Positive

- The frontend can stop displaying a raw control-panel JSON dump and
  ship the table/modal/empty-state described by the migration
  checklist.
- The interaction matrix entry CTRL-002 turns honest: routes go from
  declared-but-fake to actually implemented.
- The slice is small and additive: one new handler file, one test
  file, seven route registrations, no migration, no new dependency.
- Permission model stays consistent with sibling control-panel
  resources; no security review surface beyond the existing
  control-panel surface.

### Negative

- ADR-0035 P3 is **not** discharged. The architectural canon still
  says streaming profiles belong inside `ingestion-replication-service`.
  Anyone reading ADR-0035 will look for the migration and find this
  ADR explaining why it's elsewhere — extra cognitive load.
- `last_event_at` / `throughput_eps` are decorative until the
  telemetry source-of-truth lands. The frontend should treat them
  as best-effort and display gracefully when missing.
- In-memory storage means streaming-profile config is wiped on
  service restart, like every other control-panel resource today.
  This is acceptable in dev/preview but **not durable** — production
  rollout depends on the future durability RFC that covers all
  control-panel state.

### Migration path

When ADR-0035 P3 is picked back up (or this trade-off is reversed),
the relocation plan is:

1. Land P3 in `ingestion-replication-service` with the planned
   migration and proper repo / domain package.
2. Mount the new handlers at `/api/v1/streaming-profiles` (no
   `/control-panel` prefix).
3. Keep the parked handlers in `identity-federation-service` for
   one release as 308-redirects to the new URL.
4. Update the frontend client + interaction matrix and remove the
   parked handlers in the next release.
5. Supersede this ADR.

## Verification

- `go test ./services/identity-federation-service/...` covers handler
  contracts (CRUD + pause/resume + permission gates) via
  `streaming_profiles_test.go`.
- The frontend interaction matrix (`docs/frontend-interaction-matrix.json`)
  is updated to point CTRL-002 at the new URL prefix and reflect the
  full set of seven routes.
- No migration ships, so the migration drift check (`make ci`,
  `sqlc generate` step) is unaffected.
