# S5 Lakehouse Evidence Summary - 2026-05-03

Status: BLOCKED
Outcome: NOT CLOSED

This directory is an evidence template plus attempted validation output. It is
not a passing S5 evidence pack. Do not mark PASS until every artifact contains
output captured from a real deployed environment and the sign-off section has
two maintainers.

Update 2026-05-03T19:49:49Z: runtime code and Helm gaps found during this
run were remediated in the repo, but the connected Kubernetes context still
does not contain the S5 data-plane dependencies required for an operational
PASS.

## Run Metadata

Owner: data platform maintainers + SRE on-call
Operator: Codex local automation, on behalf of the repo owner
Environment: local Kubernetes context `default`
Kubernetes context: default
Git SHA: 9d0072167f0e7caa417a977726998968ee999be7
Start time UTC: 2026-05-03T19:49:49Z
End time UTC: 2026-05-03T19:54:59Z
Outcome: BLOCKED - missing S5 runtime environment

Sign-off status: NOT SIGNED. This pack is not approved for S5-OPS
closure.

## Code/Helm Remediation Captured In This Run

Implemented and verified before this evidence update:

- `audit-sink`, `lineage-service`, `ai-sink` and `ontology-indexer` now expose
  `/health` and `/metrics` on `METRICS_ADDR` with service-specific Prometheus
  counters/histograms.
- `lineage-service` now emits `lineage_service_records_total`,
  `lineage_service_commits_total`, `lineage_service_batch_size_records` and
  `lineage_service_lag_seconds`.
- `ai-sink` and `audit-sink` record append success/failure metrics immediately
  after Iceberg append attempts. Kafka offsets are still committed only after
  successful appends.
- `ontology-indexer` records committed Kafka records by topic/outcome and uses
  Kafka record timestamps for `ontology_indexer_lag_seconds` when present.
- Helm values now expose metrics ports for S5 consumers and set
  `TRINO_FLIGHT_SQL_URL` for `sql-bi-gateway-service`.
- `workers-go/reindex` no longer enables Kafka topic auto-creation on its
  producer client.

Verification commands:

```text
cargo test -p audit-sink --features runtime                  PASS
cargo test -p ai-sink --features runtime                     PASS
cargo test -p lineage-service                                PASS
cargo test -p ontology-indexer --features runtime            PASS
cargo test -p event-bus-data                                 PASS
cargo test -p sql-bi-gateway-service trino                   PASS
GOCACHE=/private/tmp/openfoundry-go-cache go test ./...      PASS
helm template of-apps-ops infra/k8s/helm/of-apps-ops -f infra/k8s/helm/of-apps-ops/values-prod.yaml       PASS
helm template of-data-engine infra/k8s/helm/of-data-engine -f infra/k8s/helm/of-data-engine/values-prod.yaml PASS
helm template of-ml-aip infra/k8s/helm/of-ml-aip -f infra/k8s/helm/of-ml-aip/values-prod.yaml             PASS
helm template of-ontology infra/k8s/helm/of-ontology -f infra/k8s/helm/of-ontology/values-prod.yaml       PASS
rg -n 'NotWired|not_implemented|ErrNotImplemented|todo!|TODO' \
  services/audit-sink/src services/lineage-service/src \
  services/ai-sink/src services/ontology-indexer/src \
  services/sql-bi-gateway-service/src workers-go/reindex     PASS (0 hits)
```

## Audit-Sink E2E Validation

Requested validation:

  audit.events.v1 -> audit-sink -> Iceberg of_audit.events

Required checks:

  - Real runtime audit traffic, not fixtures or direct Kafka injection.
  - Kafka append on audit.events.v1.
  - audit-sink consumer group advances and lag recovers.
  - Iceberg row count increases in of_audit.events after sink flush.
  - Schema matches event_id, at, correlation_id, kind, payload.
  - Iceberg partition is day(at).
  - No DLQ growth or audit-sink errors.
  - audit_sink_* metrics show records and successful commits.

Result:

  BLOCKED. The current cluster has no Strimzi Kafka CRDs, no kafka namespace,
  no audit-sink deployment, no Trino namespace, no Lakekeeper namespace and no
  Prometheus Operator CRDs. The visible OpenFoundry runtime services are not
  available, so no real audit.events.v1 traffic can be produced or consumed.

Captured commands and required close-out queries are in:

  docs/architecture/lakehouse-evidence/2026-05-03/iceberg-counts.sql.txt

## Lineage-Service E2E Validation

Requested validation:

  lineage.events.v1 -> lineage-service -> Iceberg of_lineage.{runs,events,datasets_io}

Required checks:

  - Real runtime OpenLineage traffic, not fixtures or direct Kafka injection.
  - Kafka append on lineage.events.v1.
  - lineage-service consumer group advances and lag recovers.
  - Iceberg row counts increase in of_lineage.runs/events/datasets_io after sink flush.
  - Trino queries return rows matching run_id/job/dataset metadata from the runtime request.
  - Table schemas match the expected lineage layout.
  - Partitions are day(started_at) for runs and day(event_time) for events/datasets_io.
  - No DLQ growth or lineage-service append/decode/commit errors.
  - Metrics/lag evidence exists for the sink.

Writer implementation status:

  PRESENT IN CODE. `services/lineage-service/src/runtime.rs` contains the
  Kafka -> Iceberg writer: it subscribes to `lineage.events.v1`, materializes
  rows for runs/events/datasets_io, calls `append_record_batches(...)` on the
  three Iceberg tables and commits Kafka offsets only after append succeeds.

Result:

  BLOCKED. The current cluster has no kafka/trino/lakekeeper/observability
  namespaces, no KafkaTopic CRD, no lineage-service deployment and no running
  Trino/Lakekeeper query path. Real lineage.events.v1 traffic cannot be
  produced, consumed, appended or queried here.

Metric status:

  RESOLVED IN CODE, NOT DEPLOYED HERE. `lineage-service` now exposes
  lineage-specific Prometheus sink metrics. Runtime sign-off still requires
  deploying the updated binary and scraping it during real traffic.

## AI-Sink E2E Validation

Requested validation:

  ai.events.v1 -> ai-sink -> Iceberg of_ai.{prompts,responses,evaluations,traces}

Required checks:

  - Real AI runtime traffic from `agent-runtime-service` or `prompt-workflow-service`.
  - Kafka append on ai.events.v1.
  - ai-sink consumer group advances and lag recovers.
  - Iceberg row counts increase in the target of_ai table(s) dictated by event kind.
  - Trino queries return rows matching event_id/run_id/trace_id/kind metadata.
  - Table schemas match the expected AI event envelope layout.
  - Partitions are day(at) for all of_ai tables.
  - No DLQ growth or ai-sink append/decode/commit errors.
  - Metrics/lag evidence exists for the sink.

Writer implementation status:

  PRESENT IN CODE. `services/ai-sink/src/runtime.rs` contains the Kafka ->
  Iceberg writer: it subscribes to `ai.events.v1`, decodes `AiEventEnvelope`,
  routes by `kind`, calls `append_record_batches(...)` on the target table and
  commits Kafka offsets only after append succeeds.

Producer contract status:

  PRESENT AS CONTRACT. `agent-runtime-service` and `prompt-workflow-service`
  both pin `TOPIC = "ai.events.v1"` and share the event envelope shape. This
  does not prove runtime traffic; the E2E run must generate events through a
  running AI service.

Result:

  BLOCKED. The current cluster has no kafka/trino/lakekeeper/observability
  namespaces, no KafkaTopic CRD, no ai-sink deployment and no AI runtime
  producer deployment. Real ai.events.v1 traffic cannot be produced, consumed,
  appended or queried here.

Metric status:

  RESOLVED IN CODE, NOT DEPLOYED HERE. `ai-sink` now registers and renders the
  pinned metric names. Runtime sign-off still requires deploying the updated
  binary and scraping it during real traffic.

## Metrics-Long E2E Validation

Requested validation:

  Prometheus remote_write -> Mimir S3 blocks -> Spark metrics aggregation ->
  Iceberg of_metrics_long.service_metrics_daily -> Trino analytical views

Required checks:

  - Real Mimir TSDB blocks exist for the materialized day.
  - `metrics-aggregation-service-daily` runs from the intended Spark job.
  - Iceberg table `of_metrics_long.service_metrics_daily` exists.
  - Table schema matches the DDL in `infra/k8s/platform/manifests/trino/views/of_metrics_long.sql`.
  - Partitioning is day(at).
  - Counts before/after prove materialization or idempotent rewrite with non-zero rows.
  - Trino analytical queries over `v_service_latency_daily` and `v_service_error_rate_daily` return rows.
  - Pipeline health shows Mimir remote-write is draining and the Spark job completed within the expected window.

Repo substrate status:

  PRESENT AS MANIFESTS/DDL. The repo contains:
    - `infra/k8s/platform/manifests/spark-jobs/metrics-aggregation-service-daily.yaml`
    - `infra/k8s/platform/manifests/trino/views/of_metrics_long.sql`
    - `infra/k8s/platform/manifests/observability/mimir/*`

Runtime implementation status:

  BLOCKING GAP. This checkout does not contain the Spark job implementation or
  packaged `metrics-aggregation-0.1.0.jar`; it is only referenced as
  `s3a://openfoundry-spark-jars/metrics-aggregation-0.1.0.jar`. The migration
  plan also notes that the runtime JAR and date cron-wrapper are deferred.

Result:

  BLOCKED. The current cluster has no `observability`, `openfoundry-spark`,
  `trino` or `lakekeeper` namespaces, and no `sparkapplications` CRD. Real
  Mimir -> Spark -> Iceberg materialization cannot be executed or queried here.

## SQL Gateway To Trino Validation

Requested validation:

  sql-bi-gateway-service routes analytical `trino.*` SELECTs to Trino and
  returns real rows from of_audit, of_lineage, of_ai and of_metrics_long.

Formal router result:

  PASS for pure routing tests. `cargo test -p sql-bi-gateway-service trino --
  --nocapture` passed the three Trino router tests:
    - `backend_all_includes_trino`
    - `missing_trino_endpoint_is_an_explicit_error`
    - `trino_routes_to_configured_endpoint`

Runtime result:

  BLOCKED. The current cluster has no `sql-bi-gateway-service` deployment, no
  `trino` namespace and no Lakekeeper/Trino runtime. Helm values now set
  `TRINO_FLIGHT_SQL_URL`, but the configured endpoint cannot be exercised in
  this Kubernetes context because the Trino Flight SQL proxy is absent.

Captured commands, SQL and close-out steps are in:

  docs/architecture/lakehouse-evidence/2026-05-03/trino-routing.txt

## Restart Drill Validation

Requested validation:

  Restart audit-sink, lineage-service, ai-sink and relevant writer/consumer
  deployments during active traffic. Verify offset recovery, no observable
  data loss, no observable duplicates or documented idempotence, lag returns to
  range and DLQ remains empty.

Result:

  BLOCKED. Restart commands were attempted against the expected S5 deployments:
  `audit-sink`, `lineage-service`, `ai-sink`, `ontology-indexer` and `reindex`.
  Every restart failed because the required namespaces (`of-apps-ops`,
  `of-data-engine`, `of-ml-aip`, `of-ontology`) do not exist in the current
  Kubernetes context. Kafka, Trino, Lakekeeper, Spark and observability
  namespaces/CRDs are also absent, so offsets, lag, DLQ, duplicate checks and
  post-restart Iceberg state could not be observed.

Captured commands and required close-out steps are in:

  docs/architecture/lakehouse-evidence/2026-05-03/restart-drill.txt

## Captured Environment Output

Deployment discovery command:

```bash
kubectl get deploy -A
```

Output:

```text
NAMESPACE            NAME                               READY   UP-TO-DATE   AVAILABLE   AGE
cert-manager         cert-manager                       1/1     1            1           12h
cert-manager         cert-manager-cainjector            1/1     1            1           12h
cert-manager         cert-manager-webhook               1/1     1            1           12h
cnpg-system          cnpg-cloudnative-pg                1/1     1            1           12h
k8ssandra-operator   k8ssandra-operator                 1/1     1            1           12h
k8ssandra-operator   k8ssandra-operator-cass-operator   1/1     1            1           12h
kube-system          coredns                            1/1     1            1           2d21h
kube-system          local-path-provisioner             1/1     1            1           2d21h
kube-system          metrics-server                     1/1     1            1           2d21h
kube-system          traefik                            1/1     1            1           2d21h
openfoundry          authorization-policy-service       0/1     1            0           12h
openfoundry          edge-gateway-service               0/1     1            0           12h
openfoundry          identity-federation-service        0/1     1            0           12h
openfoundry          tenancy-organizations-service      0/1     1            0           12h
openfoundry          web                                1/1     1            1           175m
registry             registry                           1/1     1            1           4h57m
```

Kafka CR command:

```bash
kubectl get kafkas -A
```

Output:

```text
error: the server doesn't have a resource type "kafkas"
```

KafkaTopic CR command:

```bash
kubectl get kafkatopics -A
```

Output:

```text
error: the server doesn't have a resource type "kafkatopics"
```

Trino namespace command:

```bash
kubectl get ns trino
```

Output:

```text
Error from server (NotFound): namespaces "trino" not found
```

Lakekeeper namespace command:

```bash
kubectl get ns lakekeeper
```

Output:

```text
Error from server (NotFound): namespaces "lakekeeper" not found
```

PrometheusRule command:

```bash
kubectl get prometheusrule -A
```

Output:

```text
error: the server doesn't have a resource type "prometheusrule"
```

ServiceMonitor command:

```bash
kubectl get servicemonitor -A
```

Output:

```text
error: the server doesn't have a resource type "servicemonitor"
```

## Evidence Checklist

- [ ] Kafka offsets before and after are captured in `kafka-offsets.txt`.
- [ ] audit-sink E2E counts/schema/partition/metrics are captured in `iceberg-counts.sql.txt`.
- [ ] lineage-service E2E counts/schema/partition/metrics are captured in `iceberg-counts.sql.txt`.
- [ ] ai-sink E2E counts/schema/partition/metrics are captured in `iceberg-counts.sql.txt`.
- [ ] of_metrics_long materialization counts/partitions/queries are captured in `iceberg-counts.sql.txt`.
- [ ] sql-bi-gateway-service -> Trino routing with returned rows is captured in `trino-routing.txt`.
- [ ] WORM negative test is captured in `worm-negative-test.txt`.
- [ ] Grafana links or exported panel images are captured in `grafana-snapshots.md`.
- [ ] Restart drill is captured in `restart-drill.txt`.
- [ ] No sink uses Postgres as runtime fallback for event progress, retries or checkpoint authority.

## Sign-off

- [ ] Data platform maintainer:
- [ ] SRE on-call:
- [ ] Date:
- [ ] Environment:
- [ ] Outcome: BLOCKED
