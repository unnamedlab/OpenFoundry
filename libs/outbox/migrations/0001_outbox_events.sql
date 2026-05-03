-- ADR-0022 — Transactional outbox over Postgres + Debezium.
--
-- This migration provisions the `outbox.events` table that hosts every
-- outbound domain event written by application handlers. Debezium reads
-- the WAL of this table via `pgoutput` logical decoding (see
-- `infra/docker-compose.yml` → service `debezium-connect`) and the
-- `EventRouter` SMT routes each row to the Kafka topic named by the
-- `topic` column.
--
-- Deletion strategy: the canonical Debezium outbox pattern emits an
-- INSERT immediately followed by a DELETE inside the same transaction
-- (see `libs/outbox/src/lib.rs::enqueue`). Logical decoding captures
-- both records in the WAL, the EventRouter publishes the INSERT, and
-- the DELETE is dropped (`tombstones.on.delete=false`). The table
-- therefore stays empty in steady state and we do not need a separate
-- janitor process. The plan's `outbox.event.deletion.policy=delete`
-- spelling is not an actual Debezium option; the real semantic
-- equivalent is the in-transaction INSERT+DELETE used here.
--
-- The publication and replication slot (`of_outbox_pub`,
-- `of_outbox_slot`) are created by Debezium itself on connector
-- registration (`publication.autocreate.mode=filtered`).

CREATE SCHEMA IF NOT EXISTS outbox;

CREATE TABLE IF NOT EXISTS outbox.events (
    -- Stable, deterministic id assigned by the producer (typically a
    -- v5 UUID derived from `aggregate || aggregate_id || version`).
    -- Used as the Kafka message key to guarantee per-aggregate ordering
    -- under the EventRouter SMT.
    event_id    uuid PRIMARY KEY,

    -- Domain aggregate type ("ontology_object", "dataset", …) and the
    -- aggregate instance id. Both are exposed as Kafka headers by the
    -- EventRouter so consumers can filter without parsing the payload.
    aggregate    text NOT NULL,
    aggregate_id text NOT NULL,

    -- Free-form JSON payload. Avro schemas live in Apicurio and are
    -- referenced by the consumers; the outbox table stays JSON to keep
    -- the producer side dependency-free.
    payload jsonb NOT NULL,

    -- Headers map (string → string) that the EventRouter copies onto
    -- the Kafka record. We always include the OpenLineage `ol-*`
    -- headers (run id, parent run id, namespace, job).
    headers jsonb NOT NULL DEFAULT '{}'::jsonb,

    -- Destination Kafka topic. Routed by the EventRouter SMT
    -- (`route.by.field=topic`). Examples: `ontology.object.changed.v1`,
    -- `dataset.committed.v1`.
    topic text NOT NULL,

    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS outbox.heartbeat (
    id text PRIMARY KEY,
    last_seen_at timestamptz NOT NULL DEFAULT now()
);

-- Replica identity FULL keeps both INSERT and DELETE rows readable in
-- the WAL even though we delete in the same transaction. Without this
-- the DELETE record only carries the PK, which is enough for the
-- tombstone path but breaks any future debugging tool that wants to
-- replay the table from the WAL.
ALTER TABLE outbox.events REPLICA IDENTITY FULL;
