# `pipeline-runner` (Go)

Executes a [`pipelineplan.Plan`](../../libs/pipeline-plan/) against
Iceberg via [`libs/pipeline-runtime`](../../libs/pipeline-runtime/).
ADR-0045 Phase C.5 — replaces the previous Scala/Spark binary and
its `spark-submit` shell-out with a distroless Go process.

## Plan source

The runner accepts a Plan from three sources (priority order):

| Source | Used by | Notes |
|---|---|---|
| `--plan-file <path>` flag | dev YAMLs (`infra/dev/*.yaml`) | ConfigMap mount keeps the JSON readable in `kubectl describe configmap` |
| `PIPELINE_PLAN_FILE` env var | same — env-var fallback | identical to the flag |
| `PIPELINE_PLAN_B64` env var | the dispatcher (`services/pipeline-build-service/internal/dispatch`, Phase C.4.a) | base64 JSON; the dispatcher generates Jobs that embed it |

Inside the pod, the binary decodes the Plan, runs `plan.Validate()`,
then hands it to `pipelineruntime.Executor{Reader, Writer}.Run`. The
Reader is `internal/providers/IcebergReader` (apache/iceberg-go);
the Writer is `internal/providers/HTTPWriter` (the iceberg-catalog-service
HTTP append adapter — Phase B pattern, same as audit-sink / ai-sink).

## CLI

```
pipeline-runner
  --pipeline-id        <RID>
  --run-id             <ULID>
  --input-dataset      <catalog.namespace.table>   informational, log scope
  --output-dataset     <catalog.namespace.table>   informational, log scope
  --catalog-uri        <https://...>               Iceberg REST catalog
  [--catalog-warehouse <name>]
  [--catalog-credential <user:secret>]
  [--oauth-token-uri   <url>]
  [--oauth-scope       <scope>]
  [--table-writer-url  <url>]                       defaults to --catalog-uri
  [--internal-token    <token>]                     X-Internal-Token forwarded if set
  [--plan-file         <path>]                      preferred for dev YAMLs
  [--health-addr       <host:port>]                 default 0.0.0.0:9090
  [--log-format        text|json]                   default text
  [--smoke]                                         validate Plan and exit
```

Env-var fallbacks: `ICEBERG_CATALOG_URL`, `ICEBERG_WAREHOUSE`,
`ICEBERG_CATALOG_CREDENTIAL`, `ICEBERG_OAUTH_TOKEN_URI`,
`ICEBERG_OAUTH_SCOPE`, `ICEBERG_TABLE_WRITER_URL`,
`OF_PIPELINE_RUNNER_HEALTH_ADDR`, `PORT`. The standard
`AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY` / `AWS_REGION` /
`AWS_ENDPOINT_URL_S3` / `AWS_S3_FORCE_PATH_STYLE` env vars are
honoured by the AWS SDK underneath apache/iceberg-go for S3 access.

## Endpoints

- `GET /healthz` — liveness JSON (Rust-compatible).
- `GET /metrics` — Prometheus scrape (default registry; Go runtime + process metrics).

## Phase A discoveries — bundled with the binary

The IcebergReader carries the workarounds Phase A surfaced (see
`docs/architecture/adr/ADR-0045-eliminate-pipeline-runner-spark-pure-go-runtime.md`):

- Blank import `_ "github.com/apache/iceberg-go/io/gocloud"` to register
  the S3 / GCS / Azure scheme handlers (without it any Scan against an
  `s3://` table fails with `ErrIOSchemeNotFound`).
- `rest.WithAdditionalProps(io.S3RemoteSigningEnabled: "false")` override
  to defeat Lakekeeper's advertised remote signing — apache/iceberg-go
  v0.5.0's gocloud S3 adapter does not implement it.
- `go.mod` pin: `substrait-protobuf/go@v0.81.0` because `substrait-go/v7`
  (an iceberg-go transitive) still references `extensions.SimpleExtensionURI`
  which v0.85.0 removed. `arrow-go/v18` trails along at v18.5.2.

## Build

```sh
go build -o bin/pipeline-runner ./services/pipeline-runner/cmd/pipeline-runner
```

Or via the root Makefile:

```sh
make build-services
```

## Image

```sh
docker build -t openfoundry/pipeline-runner:dev \
  -f services/pipeline-runner/Dockerfile .
```

Distroless static, nonroot, `STOPSIGNAL SIGTERM`. Image size ≈ 25 MB
(vs ≈ 700 MB on the Spark base image the prior runner used).

## Test

```sh
go test ./services/pipeline-runner/...
go test -race ./services/pipeline-runner/...
```

Unit tests cover:

- Arg parsing + env-var fallbacks (`internal/runner/args_test.go`).
- Plan-source resolution (`--plan-file`, `PIPELINE_PLAN_FILE`,
  `PIPELINE_PLAN_B64`) including bad base64, bad JSON, missing file
  (`internal/runner/args_test.go`).
- HTTPWriter against `net/http/httptest` — happy path, token
  forwarding, 404 / 409 / 422 / 5xx mapping, URL validation
  (`internal/providers/http_writer_test.go`).

`IcebergReader` has no unit tests — its surface is too tightly
coupled to `apache/iceberg-go` to fake cleanly. Smoke against a
live Lakekeeper catalog (`infra/dev/pipeline-runner-smoke.yaml`)
is the validation gate per ADR-0045 § Phase C exit criterion.

## Dev manifests

The four PoC pipelines from
[`docs/migration/pipeline-runner-spark-to-go-inventory.md`](../../docs/migration/pipeline-runner-spark-to-go-inventory.md)
are expressed as `batch/v1 Job` + `ConfigMap` pairs in
[`infra/dev/poc-pipeline-nodes.yaml`](../../infra/dev/poc-pipeline-nodes.yaml)
plus the smoke at
[`infra/dev/pipeline-runner-smoke.yaml`](../../infra/dev/pipeline-runner-smoke.yaml).
Apply with `kubectl apply -f` and watch with
`kubectl logs -l openfoundry.io/pipeline-id=… -f`.
