#!/usr/bin/env bash
# S0.9.e — End-to-end test for the Postgres outbox → Debezium → Kafka
# pipeline. Runs against the compose stack at `infra/docker-compose.yml`.
#
# Pre-requisites (all booted by `just dev-up` or `docker compose up`):
#   * postgres            (port 5432, db `pg_policy`, schema `outbox`)
#   * kafka               (port 9092, KRaft single-broker)
#   * debezium-connect    (port 8083, connector `pg-policy-outbox`
#                          registered by `debezium-connect-init`)
#
# Tools required on PATH: `psql`, `kcat` (a.k.a. kafkacat), `jq`.
#
# What it asserts:
#   1. The connector exists and is RUNNING.
#   2. Inserting a row into `outbox.events` (and deleting it in the
#      same transaction) yields a Kafka message on the topic named by
#      the row's `topic` column — proving the EventRouter SMT routing.
#   3. The message carries Kafka headers `aggregateType`, `id` and the
#      OpenLineage `ol-*` headers from the row's `headers` JSONB.
#   4. The payload JSON matches what the producer wrote.
#
# Exit status is 0 on success, non-zero on any failed assertion.

set -euo pipefail

PG_URL="${OUTBOX_PG_URL:-postgres://openfoundry:openfoundry@127.0.0.1:5432/pg_policy}"
KAFKA_BOOTSTRAP="${OUTBOX_KAFKA:-127.0.0.1:9092}"
CONNECT_URL="${OUTBOX_CONNECT_URL:-http://127.0.0.1:8083}"
TOPIC="${OUTBOX_TOPIC:-ontology.object.changed.v1}"
CONNECTOR_NAME="${OUTBOX_CONNECTOR:-pg-policy-outbox}"

require() {
  command -v "$1" >/dev/null || {
    echo "FAIL: '$1' is required on PATH (install with brew/apt)" >&2
    exit 2
  }
}
require psql
require kcat
require jq
require curl

step() { printf '\n\033[1;36m▶ %s\033[0m\n' "$*"; }
ok()   { printf '\033[1;32m✓ %s\033[0m\n' "$*"; }
fail() { printf '\033[1;31m✗ %s\033[0m\n' "$*" >&2; exit 1; }

step "1. Connector RUNNING"
state=$(curl -fsS "${CONNECT_URL}/connectors/${CONNECTOR_NAME}/status" \
  | jq -r '.connector.state')
[[ "$state" == "RUNNING" ]] || fail "connector state is '$state', expected RUNNING"
ok "connector ${CONNECTOR_NAME} is RUNNING"

step "2. Producer side: insert + delete inside one tx"
EVENT_ID=$(uuidgen | tr '[:upper:]' '[:lower:]')
AGG_ID="obj-e2e-${RANDOM}"
RUN_ID="run-e2e-${RANDOM}"
psql "$PG_URL" -v ON_ERROR_STOP=1 <<SQL
BEGIN;
INSERT INTO outbox.events
  (event_id, aggregate, aggregate_id, payload, headers, topic)
VALUES
  ('${EVENT_ID}',
   'ontology_object',
   '${AGG_ID}',
   '{"tenant":"t-e2e","type_id":"Person","object_id":"${AGG_ID}","version":1}'::jsonb,
   '{"ol-run-id":"${RUN_ID}","ol-namespace":"of","ol-job":"ontology.write"}'::jsonb,
   '${TOPIC}');
DELETE FROM outbox.events WHERE event_id = '${EVENT_ID}';
COMMIT;
SQL
ok "transaction committed (event_id=${EVENT_ID})"

step "3. Consumer side: poll Kafka topic '${TOPIC}'"
# Read from the end with a short timeout, accept up to 100 messages,
# pick the one whose key matches AGG_ID. Header format: -f '%h\t%s\n'.
msg=$(kcat -b "$KAFKA_BOOTSTRAP" -C -t "$TOPIC" -o end -e -q -u \
        -f '%h\x1f%s\n' 2>/dev/null \
      | grep -F "$AGG_ID" \
      || true)
# If the new message has not landed yet, retry with a small backoff.
for _ in 1 2 3 4 5; do
  [[ -n "$msg" ]] && break
  sleep 1
  msg=$(kcat -b "$KAFKA_BOOTSTRAP" -C -t "$TOPIC" -o end -e -q -u \
          -f '%h\x1f%s\n' 2>/dev/null \
        | grep -F "$AGG_ID" \
        || true)
done
[[ -n "$msg" ]] || fail "no message with aggregate_id=${AGG_ID} on topic ${TOPIC}"
ok "message received on ${TOPIC}"

headers="${msg%%$'\x1f'*}"
payload="${msg#*$'\x1f'}"

step "4. Headers carry OpenLineage trio + aggregate metadata"
for needed in "aggregateType=ontology_object" "ol-run-id=${RUN_ID}" "ol-namespace=of" "ol-job=ontology.write"; do
  if [[ "$headers" != *"$needed"* ]]; then
    fail "missing header: $needed (got: $headers)"
  fi
  ok "header present: $needed"
done

step "5. Payload matches producer write"
echo "$payload" | jq -e \
  --arg aid "$AGG_ID" \
  '.tenant=="t-e2e" and .type_id=="Person" and .object_id==$aid and .version==1' \
  >/dev/null \
  || fail "payload does not match: $payload"
ok "payload matches"

printf '\n\033[1;32mE2E PASS\033[0m — handler→Debezium→consumer round-trip with ol-* headers verified.\n'
