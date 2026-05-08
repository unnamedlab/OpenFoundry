# Dev Stack — Docker Compose

> **Audience**: contributors who want to bring the platform dependencies, or a
> bounded application slice, up on a laptop with Docker Compose.

The runnable Compose files live under [`infra/compose`](../../infra/compose):

- [`infra/compose/docker-compose.yml`](../../infra/compose/docker-compose.yml)
  is the current source of truth for infrastructure, application service
  profiles, volumes, health checks, and image/port environment variables.
- [`infra/compose/docker-compose.dev.yml`](../../infra/compose/docker-compose.dev.yml)
  is the optional development overlay. It exposes a `demo` profile for
  Meilisearch and lets local ports be overridden without editing the base file.

> **Known cleanup item**: the root [`compose.yaml`](../../compose.yaml) still
> delegates to the legacy `infra/docker-compose.yml` path. Until that entrypoint
> is corrected, run Compose with explicit `-f infra/compose/...` flags as shown
> below.

## Quick start

Bring up infrastructure only, without application service profiles:

```bash
docker compose \
  -f infra/compose/docker-compose.yml \
  -f infra/compose/docker-compose.dev.yml \
  up -d \
  postgres valkey nats minio minio-init vespa \
  cassandra cassandra-init opensearch kafka \
  debezium-connect debezium-connect-init \
  apicurio-registry apicurio-registry-init
```

Bring up the smallest platform application slice on top of the same backing
services:

```bash
docker compose \
  -f infra/compose/docker-compose.yml \
  -f infra/compose/docker-compose.dev.yml \
  --profile foundation \
  up -d
```

Bring up the edge profile when you need the cumulative app stack, gateway, web
frontend container, and nginx app facade:

```bash
docker compose \
  -f infra/compose/docker-compose.yml \
  -f infra/compose/docker-compose.dev.yml \
  --profile edge \
  up -d
```

To tear the stack down and wipe local volumes:

```bash
docker compose \
  -f infra/compose/docker-compose.yml \
  -f infra/compose/docker-compose.dev.yml \
  down -v
```

## Infrastructure services

| Service | Default image | Host port(s) | Healthcheck |
| --- | --- | --- | --- |
| `postgres` | `postgres:16-alpine` | `5432` | `pg_isready` |
| `valkey` | `valkey/valkey:8-alpine` | `6379` | `valkey-cli ping` |
| `nats` | `nats:2-alpine` | `4222`, `8222` | `GET /healthz` on the monitor port |
| `minio` / `minio-init` | `minio/minio:latest`, `minio/mc:latest` | `9000`, `9001` | `mc ready` and bucket bootstrap |
| `vespa` | `vespaengine/vespa:8.450.30` | `18080`, `19071` | `GET /state/v1/health` |
| `cassandra` / `cassandra-init` | `cassandra:5.0` | `9042` | `cqlsh -e 'DESCRIBE KEYSPACES'` |
| `opensearch` | `opensearchproject/opensearch:2.18.0` | `9200`, `9600` | `_cluster/health` |
| `kafka` | `apache/kafka:3.7.1` | `9092` | `kafka-topics --list` |
| `debezium-connect` / `debezium-connect-init` | `debezium/connect:2.7`, `curlimages/curl:8.10.1` | `8083` | connector registration/status |
| `apicurio-registry` / `apicurio-registry-init` | `apicurio/apicurio-registry-mem:2.6.4.Final`, `curlimages/curl:8.10.1` | `8084` | registry liveness plus subject bootstrap |
| `meilisearch` | `getmeili/meilisearch:v1.11` | `7700` | optional `demo` profile only |

Override images and host ports with the `OPENFOUNDRY_*` environment variables
that are documented inline in the Compose files. For example:

```bash
OPENFOUNDRY_CASSANDRA_HOST_PORT=19042 \
OPENFOUNDRY_POSTGRES_HOST_PORT=15432 \
docker compose -f infra/compose/docker-compose.yml up -d cassandra postgres
```

## Cassandra

The local stack uses a single `cassandra:5.0` node with
`GossipingPropertyFileSnitch` and `dc1/rack1`. The `cassandra-init` one-shot
container waits for CQL readiness and creates the local application keyspaces
with `RF=1`.

```bash
docker compose -f infra/compose/docker-compose.yml exec cassandra cqlsh
```

Production topology, replication, and operational procedures remain documented
in [`infra/runbooks/cassandra.md`](../../infra/runbooks/cassandra.md) and
[ADR-0020](../architecture/adr/ADR-0020-cassandra-as-operational-store.md).

## Search backends

Vespa is the primary local search/vector backend in the base stack. OpenSearch
is also available as a compatibility and CI fallback for paths routed through
`libs/search-abstraction`.

```bash
curl http://localhost:18080/state/v1/health | jq
curl http://localhost:9200/_cluster/health | jq
```

The development overlay also contains Meilisearch behind the optional `demo`
profile:

```bash
docker compose \
  -f infra/compose/docker-compose.yml \
  -f infra/compose/docker-compose.dev.yml \
  --profile demo \
  up -d meilisearch
```

## Postgres, Kafka, Debezium, and Apicurio

Postgres starts with logical-decoding settings required by Debezium:
`wal_level=logical`, `max_replication_slots=4`, and `max_wal_senders=4`.
`POSTGRES_MULTIPLE_DATABASES` includes `pg_policy` by default so the
transactional outbox database exists before connector registration.

Kafka runs as a single KRaft broker on `kafka:9092` inside the Compose network
and `localhost:9092` from the host. Debezium Connect registers the
`pg-policy-outbox` connector and publishes topics prefixed with
`openfoundry.pg_policy.*`. Apicurio Registry is available on `localhost:8084`
for schema-registry-compatible development flows.

```bash
curl http://localhost:8083/connectors/pg-policy-outbox/status | jq
curl http://localhost:8084/apis/registry/v2/system/info | jq

docker compose -f infra/compose/docker-compose.yml exec kafka \
  /opt/kafka/bin/kafka-console-consumer.sh \
  --bootstrap-server localhost:9092 \
  --topic openfoundry.pg_policy.outbox.events \
  --from-beginning
```

## Application profiles

Application services are gated behind cumulative profiles:
`foundation` ⊂ `data` ⊂ `knowledge` ⊂ `intelligence` ⊂ `experience` ⊂ `edge`.
Infra services have no profile and can be started independently.

| Profile | Adds |
| --- | --- |
| `foundation` | identity, authorization, tenancy, notifications, and audit-compliance services |
| `data` | connectors, ingestion, datasets, media sets, SQL/BI, workflow automation, pipeline build, lineage, ontology definition, and ontology actions |
| `knowledge` | object database, ontology query, and entity resolution services |
| `intelligence` | agent runtime, exploratory analysis, notebooks, model catalog/deployment, AI evaluation, LLM catalog, and retrieval context |
| `experience` | application composition, solution design, SDK generation, telemetry governance, code review, and federation/product exchange |
| `edge` | gateway, web frontend container, and nginx app facade |

## Local resource budget

The base infrastructure stack is intentionally laptop-oriented but still
requires several GB of memory. The largest steady-state consumers are Vespa,
Cassandra, OpenSearch, Kafka, and Postgres. Start only the named services or the
lowest application profile you need when iterating on a bounded area.

## Relationship to helper scripts

[`infra/scripts/dev-stack.sh`](../../infra/scripts/dev-stack.sh) provisions
ports, writes `.openfoundry/dev-stack.env`, starts Compose infrastructure, and
runs selected binaries plus the web app from the host. It is useful for manual
end-to-end verification, but the explicit Docker Compose commands above are the
most direct way to inspect and control the backing services.
