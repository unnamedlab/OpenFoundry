#!/bin/bash
# ADR-0022 — provision the outbox schema in the `pg_policy` database.
#
# The `01-create-databases.sh` script already created `pg_policy`.
# Here we apply the same DDL shipped as a sqlx migration in
# `libs/outbox/migrations/0001_outbox_events.sql`. Keep the two files
# byte-equivalent for the table definition itself; CI checks that
# `enqueue()` round-trips against the dev compose database.
set -euo pipefail

if [ -z "${POSTGRES_MULTIPLE_DATABASES:-}" ] || ! echo ",${POSTGRES_MULTIPLE_DATABASES}," | grep -q ",pg_policy,"; then
  echo "pg_policy database not requested; skipping outbox schema provisioning."
  exit 0
fi

echo "Provisioning outbox.events in pg_policy…"
psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname pg_policy <<'SQL'
CREATE SCHEMA IF NOT EXISTS outbox;

CREATE TABLE IF NOT EXISTS outbox.events (
    event_id     uuid PRIMARY KEY,
    aggregate    text NOT NULL,
    aggregate_id text NOT NULL,
    payload      jsonb NOT NULL,
    headers      jsonb NOT NULL DEFAULT '{}'::jsonb,
    topic        text NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE outbox.events REPLICA IDENTITY FULL;
SQL
echo "Done."
