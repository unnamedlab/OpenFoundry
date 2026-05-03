# Archived `event-streaming-service` runtime migrations

This folder collects the **legacy Postgres hot-path DDL** that was
authoritative before the streaming runtime moved state out of the SQL
control plane.

## What changed

- `streaming_events`, `streaming_checkpoints`,
  `streaming_cold_archives` and `streaming_topology_checkpoints` are
  no longer created from the active Postgres migration chain.
- The active chain keeps only the surviving control-plane column
  additions:
  `20260502130000_streaming_archive_controls.sql` and
  `20260502140000_streaming_runtime_controls.sql`.
- The authoritative hot path now lives in the runtime store owned by
  `event-streaming-service` (`runtime_store.rs`) together with the
  configured hot buffer backend.
- Postgres remains the control plane for long-lived stream, topology,
  window, branch, schema and Flink deployment metadata.
- The active migration
  `services/event-streaming-service/migrations/20260502210000_drop_postgres_streaming_runtime.sql`
  stays in place so existing environments can drop the legacy tables
  during cutover.

## Archived files

- `20260424232000_streaming_runtime.sql`
- `20260502130000_streaming_cold_archives.sql`
- `20260502140000_streaming_topology_checkpoints.sql`

## Pointers

- Runtime store: [`services/event-streaming-service/src/domain/runtime_store.rs`](../../../services/event-streaming-service/src/domain/runtime_store.rs)
- Plan reference: `docs/architecture/migration-plan-cassandra-foundry-parity.md`

> **Do not resurrect** these tables under the active Postgres
> migration chain. New runtime-state persistence belongs in the
> streaming runtime store, not in `services/event-streaming-service/migrations/`.
