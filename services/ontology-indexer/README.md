# ontology-indexer

`ontology-indexer` is a Kafka worker that projects ontology object and link change
events into the configured search backend. The HTTP server is operational only:
`/healthz` and `/metrics` do not expose application routes.

## Minimal runtime configuration

Set these environment variables before starting the service:

| Variable | Required | Default | Description |
| --- | --- | --- | --- |
| `KAFKA_BOOTSTRAP_SERVERS` | yes | — | Comma-separated Kafka brokers used by the consumer and DLQ publisher, for example `kafka-1:9092,kafka-2:9092`. |
| `SEARCH_ENDPOINT` | yes | — | Base URL for the configured search backend. |
| `SEARCH_BACKEND` | recommended | `vespa` | Search backend implementation. Supported values are `vespa` and `opensearch`; unset values keep the Vespa-compatible default. |
| `INDEXER_DLQ_TOPIC` | recommended | `ontology-indexer.dlq.v1` | Dead-letter topic for records that still fail after retries. Set to `off`, `none`, or `disabled` to fail the worker instead of publishing to the DLQ. |

Optional authentication variables for the search backend are applied in priority
order: `SEARCH_BEARER_TOKEN`, then `SEARCH_API_KEY`, then
`SEARCH_USERNAME`/`SEARCH_PASSWORD` for basic auth.

## Runtime behavior

On startup the worker subscribes to:

- `ontology.objects.changed.v1`
- `ontology.links.changed.v1`

Records are committed only after successful projection into the SearchBackend or
after publishing a failed record to the DLQ. Malformed payloads are treated as
decode errors, logged, and committed so a poison record does not block the
partition.

## Real Kafka integration test

The unit tests use fake readers and backends. The real Kafka integration test is
kept but is skipped unless `KAFKA_BOOTSTRAP_SERVERS` is present:

```sh
KAFKA_BOOTSTRAP_SERVERS=localhost:9092 go test ./services/ontology-indexer/internal/runtime -run TestConsumerWithRealKafkaValidMalformedAndRetry
```

## OSV2 row projection mode

In OSV2 deployments the worker can be wired with a `runtime.StorageProjector`
(`ObjectStore` plus `LinkStore`) by calling `runtime.RunWithStores`. In this
mode each Kafka record is applied to OSV2 object/link rows before the search
projection. The runtime tracks `event_id` values and aggregate versions in the
projection index, so duplicate producer retries are skipped and Kafka offsets are
committed only after all configured row and search writes succeed.
