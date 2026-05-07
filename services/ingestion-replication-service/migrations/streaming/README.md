# Active `event-streaming-service` migrations

This folder is the **active Postgres control-plane chain** for
`event-streaming-service`.

## What belongs here

- Long-lived metadata for streams, topologies, windows, branches,
  schemas, Flink deployment state and related control-plane settings.
- Column additions that configure the runtime from Postgres, such as
  archive cadence, topology checkpoint cadence and stream profiles.
- Transitional cleanup like
  `20260502210000_drop_postgres_streaming_runtime.sql`, which removes
  legacy runtime tables during cutover.

## What does not belong here

- Hot event rows (`streaming_events`)
- Runtime checkpoint/offset ledgers (`streaming_checkpoints`,
  `streaming_topology_checkpoints`)
- Cold archive bookkeeping (`streaming_cold_archives`)

Those tables are archived under
`docs/architecture/legacy-migrations/event-streaming-service/` to keep
the pre-cutover DDL history without presenting Postgres as the current
runtime store.
