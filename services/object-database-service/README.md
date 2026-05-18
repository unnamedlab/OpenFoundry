# object-database-service

## LLM context

Owns ontology object/link instance storage and query/traversal compatibility APIs.

Agent note: supports Cassandra-backed storage and dev stub modes; ontology-definition is used for type/property context when configured.

## Entrypoints

- `cmd/object-database-service/main.go` builds the `object-database-service` binary.

## Current HTTP / runtime surface

- `/api/v1/object-database/objects/{tenant}*`
- `/api/v1/object-database/links/{tenant}*`
- `/api/v1/ontology/types/{type_id}/objects*`
- `POST /api/v1/ontology/types/{type_id}/links/traverse`
- `GET /healthz`
- `GET /metrics`

## State and dependencies

- No SQL migration files live under this service directory.
- Main internal packages: `cedarauthz`, `config`, `handlers`, `server`, `storage`.
- Local service files present: `Dockerfile`.

## Configuration signals

Environment variables referenced by the code:
- `ALLOW_SUBSTRATE_STUBS`, `CASSANDRA_ADDR`, `CASSANDRA_CONTACT_POINTS`, `CASSANDRA_KEYSPACE`, `CASSANDRA_LINK_KEYSPACE`, `CASSANDRA_LOCAL_DC`, `CASSANDRA_OBJECT_KEYSPACE`, `CASSANDRA_PASSWORD`
- `CASSANDRA_USERNAME`, `HOST`, `OBJECT_DATABASE_BACKEND`, `OF_DEV_STUB_MODE`, `ONTOLOGY_DEFINITION_SERVICE_URL`, `PORT`, `SERVICE_VERSION`

## Build

```sh
go build -o bin/object-database-service ./services/object-database-service/cmd/object-database-service
```

## Before editing

- Start from the entrypoint and `internal/server`/`internal/handlers` to confirm mounted routes.
- Treat this README as a map of the current code, not a product spec; update it in the same PR as behavior changes.
- Prefer existing shared libraries under `libs/` for auth, observability, storage, and generated contracts instead of duplicating patterns.
