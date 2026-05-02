# Dev Stack — `compose.yaml`

> **Audience**: contributors that want to bring the platform up on
> their laptop with `docker compose up`.

OpenFoundry's local development stack is defined in
[`compose.yaml`](../../compose.yaml) at the repo root, which delegates
to [`infra/docker-compose.yml`](../../infra/docker-compose.yml). One
command brings up every external dependency the workspace needs to
build and run end to end.

## Quick start

```bash
# Bring up infrastructure only (no application services).
docker compose up -d \
    postgres valkey nats minio minio-init vespa \
    cassandra cassandra-init temporal temporal-ui \
    opensearch kafka debezium-connect debezium-connect-init

# Bring up the foundation profile of application services on top.
docker compose --profile foundation up -d
```

To tear everything down (and wipe volumes):

```bash
docker compose down -v
```

## Infrastructure services

| Service               | Image                                | Host port(s)        | Healthcheck                                     |
|-----------------------|--------------------------------------|---------------------|-------------------------------------------------|
| `postgres`            | `postgres:16-alpine`                 | `5432`              | `pg_isready`                                    |
| `valkey`              | `valkey/valkey:8-alpine`             | `6379`              | `valkey-cli ping`                               |
| `nats`                | `nats:2-alpine`                      | `4222`, `8222`      | `wget /healthz`                                 |
| `minio`               | `minio/minio:latest`                 | `9000`, `9001`      | `mc ready`                                      |
| `vespa`               | `vespaengine/vespa:8.x`              | `18080`, `19071`    | `GET /state/v1/health`                          |
| `cassandra`           | `cassandra:5.0`                      | `9042`              | `cqlsh -e 'DESCRIBE KEYSPACES'`                 |
| `temporal`            | `temporalio/auto-setup:1.24`         | `7233`              | `tctl cluster health`                           |
| `temporal-ui`         | `temporalio/ui:2.31.2`               | `8233`              | (depends on `temporal`)                         |
| `opensearch`          | `opensearchproject/opensearch:2.18`  | `9200`, `9600`      | `_cluster/health` ≥ yellow                      |
| `kafka`               | `apache/kafka:3.7` (KRaft)           | `9092`              | `kafka-topics --list`                           |
| `debezium-connect`    | `debezium/connect:2.7`               | `8083`              | `GET /`                                         |

Override any image or host port via the standard environment
variables documented inline in [`infra/docker-compose.yml`](../../infra/docker-compose.yml)
(e.g. `OPENFOUNDRY_CASSANDRA_HOST_PORT=19042`).

## Cassandra (S0.6.a–b)

Single-node `cassandra:5.0` with `GossipingPropertyFileSnitch` and
`dc1/rack1`. Production runs the full 3-DC × 3-rack × RF=3 layout
managed by `k8ssandra-operator` — see
[`infra/runbooks/cassandra.md`](../../infra/runbooks/cassandra.md) and
[ADR-0020](../architecture/adr/ADR-0020-cassandra-as-operational-store.md).

The `cassandra-init` container runs once after the node reports
healthy and creates the six application keyspaces with `RF=1`:

- `ontology_objects`
- `ontology_indexes`
- `actions_log`
- `auth_runtime`
- `notifications_inbox`
- `agent_state`

`temporal_persistence` and `temporal_visibility` are created by the
Temporal `auto-setup` container itself; they intentionally do not
appear in the init list.

The healthcheck uses `cqlsh -e 'DESCRIBE KEYSPACES'` rather than just
a port probe because Cassandra accepts TCP connections several
seconds before CQL is actually ready.

```bash
# Manual cqlsh session.
docker compose exec cassandra cqlsh
```

## Temporal (S0.6.c)

`temporalio/auto-setup:1.24` is the all-in-one image that bootstraps
the namespace and runs the schema creation against Cassandra. The
dev stack pins the same major version as production
([ADR-0021](../architecture/adr/ADR-0021-temporal-on-cassandra-go-workers.md))
so workflows authored against the dev frontend work unchanged on the
prod cluster.

- gRPC frontend: `localhost:7233`
- Web UI: <http://localhost:8233>
- Default namespace: `default`, retention `1d`

The container declares `cassandra-init` as a `service_completed_successfully`
dependency so Temporal does not try to migrate its keyspaces before
the application keyspaces exist.

```bash
# Inspect with the Temporal CLI from the host.
brew install temporalio/brew/temporal   # one-off
temporal workflow list --address localhost:7233
```

## OpenSearch (S0.6.d)

Single-node OpenSearch 2.18 with the security plugin disabled. This
is the **dev / CI fallback** for the production Vespa search backend
per [ADR-0028](../architecture/adr/ADR-0028-search-backend-abstraction.md);
it is selected at runtime via the `SearchBackend` trait in
`libs/search-abstraction`. Heap is capped at 512 MB so the whole
stack still fits comfortably on a developer laptop.

```bash
# Cluster health.
curl http://localhost:9200/_cluster/health | jq
```

## Postgres + Debezium (S0.6.e–f)

The `postgres` container is started with `wal_level=logical`,
`max_replication_slots=4` and `max_wal_senders=4`, which are the
prerequisites for the Debezium Postgres connector. The
`POSTGRES_MULTIPLE_DATABASES` default now includes `pg_policy`, the
database that hosts the transactional outbox per
[ADR-0022](../architecture/adr/ADR-0022-transactional-outbox-postgres-debezium.md).

Kafka runs in single-broker KRaft mode (no ZooKeeper) on
`kafka:9092` (`localhost:9092` from the host).

`debezium-connect` is a vanilla Kafka Connect distribution; the
`debezium-connect-init` one-shot container registers the
`pg-policy-outbox` connector against
`http://debezium-connect:8083/connectors/`. The connector:

- Reads from `pg_policy` via the `pgoutput` plugin.
- Captures the `outbox.events` table (default) plus `public.outbox_events`
  for compatibility.
- Publishes to topics prefixed with `openfoundry.pg_policy.*`.
- Uses replication slot `of_outbox_slot` and publication `of_outbox_pub`
  (`publication.autocreate.mode=filtered`).

To verify the connector is running:

```bash
curl http://localhost:8083/connectors/pg-policy-outbox/status | jq
```

To consume the resulting topic:

```bash
docker compose exec kafka /opt/kafka/bin/kafka-console-consumer.sh \
    --bootstrap-server localhost:9092 \
    --topic openfoundry.pg_policy.outbox.events \
    --from-beginning
```

If you bump `wal_level` or replication settings, you must restart the
`postgres` container and recreate the replication slot — see the
Debezium runbook in
[`infra/runbooks/cassandra.md`](../../infra/runbooks/cassandra.md)
for the procedure (the same logical-decoding mechanics apply).

## Resource budget

Default heap caps and memory hints, in rough descending order:

| Service       | RAM (steady)  |
|---------------|---------------|
| `vespa`       | ~1.5 GB       |
| `cassandra`   | ~1.2 GB       |
| `opensearch`  | ~1.0 GB       |
| `kafka`       | ~700 MB       |
| `temporal`    | ~400 MB       |
| `postgres`    | ~250 MB       |
| Everything else | <200 MB each |

Total infra footprint is around 6–7 GB. Application services add a
modest amount on top because each Rust service is built statically.

## Profiles

Application services are gated behind cumulative profiles:
`foundation` ⊂ `data` ⊂ `knowledge` ⊂ `intelligence` ⊂ `experience` ⊂
`edge`. Bring up `foundation` for the auth + tenancy slice, `data`
for ingestion + dataset versioning, and so on. Infra services have
no profile — they always come up.

```bash
docker compose --profile foundation up -d   # auth, tenancy, …
docker compose --profile data       up -d   # foundation + data engineering
docker compose --profile edge       up -d   # everything
```
