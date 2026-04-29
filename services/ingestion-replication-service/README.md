# ingestion-replication-service

Kubernetes-native **control plane** for streaming ingestion. The service does
**not** move bytes itself — it accepts an `IngestJobSpec` over gRPC and
materialises the corresponding workloads as Kubernetes Custom Resources:

* a [Strimzi](https://strimzi.io/) **`KafkaConnector`** (`kafka.strimzi.io/v1beta2`)
  running the [Debezium](https://debezium.io/) PostgreSQL connector
  (`io.debezium.connector.postgresql.PostgresConnector`), attached to a
  pre-existing `KafkaConnect` cluster.
* an optional [Apache Flink Kubernetes Operator](https://nightlies.apache.org/flink/flink-kubernetes-operator-docs-stable/)
  **`FlinkDeployment`** (`flink.apache.org/v1beta1`, Apache-2.0) running an
  [iceberg-flink](https://iceberg.apache.org/docs/latest/flink/) sink job that
  consumes the CDC topic and writes to an Apache Iceberg table.

A simple reconcile loop re-applies the resources for any persisted job whose
status is not terminal, so the desired state is restored automatically if a
human or another controller deletes the CRDs.

## Architecture

```
                                    ┌──────────────────────────┐
                                    │   Postgres (this svc)    │
                                    │  • ingest_jobs table     │
                                    └──────────────▲───────────┘
                                                   │
            gRPC                                   │
 ┌─────────┐  CreateIngestJob   ┌──────────────────┴───────────┐  SSA  ┌──────────────┐
 │ client  │ ─────────────────► │ IngestionControlPlane (Rust) │ ────► │   Kube API   │
 └─────────┘                    │  • render_resources          │       │              │
                                │  • apply / delete            │       │ • KafkaConn. │
                                │  • reconcile loop            │       │ • FlinkDepl. │
                                └──────────────────────────────┘       └──────────────┘
                                                                             │
                                                                             ▼
                                              Strimzi KafkaConnect ── Debezium ── Kafka topics
                                                                             │
                                                                             ▼
                                              Flink Operator ── iceberg-flink ── Iceberg / S3
```

## gRPC API

Defined in [`proto/ingestion_control_plane.proto`](proto/ingestion_control_plane.proto):

| RPC                 | Behaviour                                                                                  |
| ------------------- | ------------------------------------------------------------------------------------------ |
| `CreateIngestJob`   | Validates the spec, persists the job, renders + server-side-applies the CRDs.              |
| `GetIngestJob`      | Returns a single persisted job by id.                                                      |
| `ListIngestJobs`    | Lists every persisted job.                                                                 |
| `DeleteIngestJob`   | Removes the row and best-effort deletes the associated `KafkaConnector`/`FlinkDeployment`. |

### `IngestJobSpec`

```text
name                   logical job name (used to derive resource names)
namespace              target Kubernetes namespace (defaults to env DEFAULT_NAMESPACE)
source                 currently only "postgres"
postgres               PostgresSource block
kafka_connect_cluster  Strimzi KafkaConnect cluster to attach the connector to
iceberg_sink           optional IcebergSink — when present a FlinkDeployment is also created
```

For Postgres, the connector is configured with the `pgoutput` plugin, an
external secret reference for `database.password`
(`${secrets:<password_secret>/password}`) and the requested
`slot.name` / `publication.name` / `table.include.list` / `topic.prefix`.

For the optional Iceberg sink, a `FlinkDeployment` is created with the
configured `flink_image` (default `apache/flink:1.18-scala_2.12-java11`) and
its job is invoked with the warehouse / catalog / database / table
coordinates. The image is expected to bundle the `iceberg-flink-runtime` JAR
under `local:///opt/flink/usrlib/iceberg-sink.jar` (Apache-2.0).

## Persistence

Each accepted spec is upserted into the `ingest_jobs` table (migration
[`migrations/20260429120000_ingest_jobs.sql`](migrations/20260429120000_ingest_jobs.sql))
following the same `id (UUID v7) + JSONB spec + status` pattern used by the
other services in the workspace (see for instance `cdc-metadata-service`).

Status transitions are: `pending → materialized` on success, `pending → failed`
or `materialized → failed` when a reconcile/apply fails. The reconcile loop
periodically re-applies resources for jobs in any non-terminal state.

## Configuration

Environment variables (read via `config` with `__` separator):

| Variable                  | Default     | Purpose                                       |
| ------------------------- | ----------- | --------------------------------------------- |
| `HOST`                    | `0.0.0.0`   | gRPC bind address                             |
| `PORT`                    | `50090`     | gRPC bind port                                |
| `DATABASE_URL`            | *required*  | Postgres connection string                    |
| `DEFAULT_NAMESPACE`       | `default`   | Used when an `IngestJobSpec` omits namespace  |
| `RECONCILE_PERIOD_SECS`   | `30`        | Reconcile loop interval                       |

The Kubernetes client uses the standard `KUBECONFIG` / in-cluster service
account discovery (`kube::Client::try_default`).

## Running

```bash
# Local (requires a reachable Postgres + a kubeconfig pointing at a cluster
# with the Strimzi & Flink-Operator CRDs installed):
DATABASE_URL=postgres://localhost/ingest cargo run -p ingestion-replication-service
```

In production, the workload is shipped as a container — see [`Dockerfile`](Dockerfile).
The Dockerfile installs `protobuf-dev` because the build script
([`build.rs`](build.rs)) compiles the gRPC proto via `tonic-build`.

## Testing

```bash
cargo test -p ingestion-replication-service
```

Two layers of tests:

* **Unit**, in [`src/control_plane.rs`](src/control_plane.rs): the pure
  `render_resources` helper that turns an `IngestJobSpec` into the typed CRD
  objects. These cover the happy paths and the validation errors (empty name,
  unsupported source, missing postgres block).
* **Kube fake / stub**, in [`tests/control_plane_kube_stub.rs`](tests/control_plane_kube_stub.rs):
  builds a `kube::Client` from a `tower_test::mock::pair` so we can assert on
  the exact HTTP requests `apply_resources` issues (path, method,
  `application/apply-patch+yaml` content-type, server-side-apply field
  manager and JSON body) without a real API server.

## Dependencies of note

* [`kube`](https://docs.rs/kube) `0.95` — Apache-2.0 — typed Kubernetes client.
* [`k8s-openapi`](https://docs.rs/k8s-openapi) `0.23` — Apache-2.0 — typed
  OpenAPI definitions, pinned to `v1_30`.
* [`tonic`](https://docs.rs/tonic) — gRPC server.
* [`schemars`](https://docs.rs/schemars) `0.8` — required by
  `kube::CustomResource`.

All vetted via the GitHub Advisory Database before being added.
