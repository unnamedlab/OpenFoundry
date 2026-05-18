# `iceberg-object-indexer` (Go)

## LLM quick context (current code)

Background indexer that projects object/ontology changes into Iceberg/object-index tables.

Agent note: not a user-facing API service; health/metrics plus worker runtime.

Current surface:
- `GET /healthz`
- `GET /metrics`

State/dependency hints:
- No SQL migration files live under this service directory.
- Main internal packages: `runner`, `server`, `sink`, `source`.
- Local service files present: `Dockerfile`.

Configuration signals:
No direct `os.Getenv` / `os.LookupEnv` references were detected in service Go files; inspect shared config loaders if behavior is unclear.

Keep this section in sync when changing routes, config, or persistence behavior.

Reads rows from an Iceberg table via the Lakekeeper REST catalog and
PUTs each row into `object-database-service` at
`/api/v1/object-database/objects/{tenant}/{id}`.

Phase A of [ADR-0045](../../docs/architecture/adr/ADR-0045-eliminate-pipeline-runner-spark-pure-go-runtime.md) —
this binary replaces the Scala
`com.openfoundry.indexer.IcebergToObjectStoreIndexer` previously
packaged in `services/pipeline-runner-spark/`. CLI surface is
flag-for-flag identical so the SparkApplication CR can be swapped
for a plain `Job` without operator-facing changes (see the original
`infra/dev/indexer-online-retail.yaml`, now reworked to use this
image).

## Why this is the first Phase

`IcebergToObjectStoreIndexer` is the smallest leaf node in the Spark
perimeter: read-only Iceberg scan + per-row HTTP PUT. The Phase A
exit criterion of ADR-0045 — *"apache/iceberg-go handles every read
path we need at production scale"* — is validated here.

## CLI

Identical to the Scala port (matched flag-for-flag) plus four new
flags that the SparkApplication CR previously injected as
`spark.sql.catalog.lakekeeper.*` sparkConf:

```
--source-table       <iceberg ref>  e.g. lakekeeper.default.transactions_clean
--target-tenant      <string>       default: "default"
--target-type-id     <uuid>         ontology object_type_id
--id-column          <string>       row column carrying the object id
--object-database-url <url>          default: http://object-database-service.openfoundry.svc:8080
[--internal-token    <string>]      optional X-Internal-Token header
[--catalog           <string>]      default: "lakekeeper"
[--catalog-uri       <url>]         default: http://lakekeeper.lakekeeper.svc:8181/catalog
[--catalog-warehouse <string>]      e.g. "openfoundry"
[--catalog-credential <user:secret>] e.g. "lakekeeper:s" (basic credential for the REST catalog)
[--oauth-token-uri   <url>]         optional OAuth2 token endpoint
[--oauth-scope       <string>]      optional OAuth2 scope, e.g. "openid"
[--limit             <int64>]       cap rows for smoke runs (0 = no cap)
[--smoke]                           validate args; skip iceberg read + object-database writes
[--health-addr       <host:port>]   default: 0.0.0.0:9090
[--log-format        text|json]     default: text
```

## Endpoints

- `GET /healthz` — liveness JSON: `{"status":"ok","service":"iceberg-object-indexer","version":"…"}`.
- `GET /metrics` — Prometheus scrape from a service-local registry (no global state).

## Metrics

| Name | Type | Labels | Meaning |
|---|---|---|---|
| `iceberg_object_indexer_rows_total` | counter | `outcome=success\|client_error\|server_error\|skipped` | rows the runner has processed |
| `iceberg_object_indexer_batches_total` | counter | — | Arrow record batches consumed from the source |
| `iceberg_object_indexer_duration_seconds` | histogram | — | end-to-end indexing pass duration |

## Failure model

- **2xx from `object-database-service`** → counted as `success`.
- **4xx** → counted as `client_error`. The row is logged with status + body and the run keeps going (matches the Scala `System.err.println` path).
- **5xx** → counted as `server_error`. Same — keep going so a transient batch does not lose the entire scan.
- **Transport / connection errors** → fatal. The run aborts with a wrapped error and a non-zero exit code so the surrounding `Job` is marked Failed.
- **Iceberg scan errors** → fatal. Same exit-code behaviour.
- **Rows missing the id column or with empty id** → counted as `skipped` (logged at WARN). Matches the Scala `if (id != null && id.nonEmpty)` guard.

## Build

```sh
go build -o bin/iceberg-object-indexer ./services/iceberg-object-indexer/cmd/iceberg-object-indexer
```

Or via the root Makefile (auto-picked up by the `wildcard` rule):

```sh
make build-services
```

## Image

```sh
docker build -t openfoundry/iceberg-object-indexer:dev \
  -f services/iceberg-object-indexer/Dockerfile .
```

The Dockerfile mirrors `services/template/Dockerfile`: distroless static, nonroot UID 65532, `STOPSIGNAL SIGTERM`, OCI labels for SBOM/registry.

## Test

```sh
go test ./services/iceberg-object-indexer/...
go test -race ./services/iceberg-object-indexer/...
```

Unit tests cover:

- Arg parsing + validation (`internal/runner/args_test.go`).
- Runner orchestration with `Source` + `Sink` fakes — happy path, ID-stringification edge cases (Arrow int / float / bool / json.Number / bytes / nil / empty), 4xx-keeps-going, 5xx-keeps-going, transport-error-aborts, scan-error-aborts (`internal/runner/runner_test.go`).
- `ObjectDB` HTTP client against `net/http/httptest` — happy path, token-omitted-when-empty, 422 surfaces as `*HTTPError`, path-segment URL escaping, URL validation (`internal/sink/objectdb_test.go`).
- Iceberg table-ident parser — catalog-prefix stripping, nested namespaces, error cases (`internal/source/iceberg_test.go`).

End-to-end smoke against a live Lakekeeper + Iceberg table is the
exit criterion of [ADR-0045 Phase A](../../docs/architecture/adr/ADR-0045-eliminate-pipeline-runner-spark-pure-go-runtime.md);
run it from a dev k3s cluster against `lakekeeper.default.transactions_clean`
using the manifest at `infra/dev/indexer-online-retail.yaml`.

## Dependency notes

Adding `apache/iceberg-go@v0.5.0` to `go.mod` pinned two transitive
versions downward to keep the build graph consistent:

- `github.com/substrait-io/substrait-protobuf/go` → **v0.81.0** (was v0.85.0 at MVS-time). v0.85.0 removed `extensions.SimpleExtensionURI`, which `substrait-go/v7@v7.4.0` (an iceberg-go transitive) still references.
- `github.com/apache/arrow-go/v18` → **v18.5.2** (was v18.6.0). Pulled along by the substrait downgrade.

Both pins are verified by `go build ./...` across the full repo
(see ADR-0045 Phase A exit criterion). If a future bump of iceberg-go
breaks this, the substitution rules belong in `go.mod` rather than a
fork.
