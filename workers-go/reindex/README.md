# reindex worker

> Stream: S4 · Tarea S4.3.d
> Mirrors: [`workers-go/workflow-automation`](../workflow-automation)

Temporal Go worker that drives the **OntologyReindex** workflow:
scans the Cassandra ontology store and publishes batches to the
dedicated topic `ontology.reindex.v1`. The
[`ontology-indexer`](../../services/ontology-indexer) consumer group
then materialises them into Vespa / OpenSearch.

## Why a separate topic

Backfill traffic on the live `ontology.object.changed.v1` topic
would starve the live consumer group during a full reindex. Same
payload shape, separate topic — one consumer group reads both.

## Why a separate worker binary

A reindex run can take hours and continue-as-new every 50k records.
Isolating it from `workflow-automation` keeps the live workflow
worker latency-bounded.

## Contract pinning

Constants in
[`internal/contract/contract.go`](internal/contract/contract.go)
**must match** byte-for-byte:

* The Rust `libs/temporal-client::task_queues::REINDEX` (added in
  the same PR that wires this worker into the cluster).
* The topic name in
  [`services/ontology-indexer::topics::ONTOLOGY_REINDEX_V1`](../../services/ontology-indexer/src/lib.rs).

## Runtime

The worker pages `ontology_objects.objects_by_type`, hydrates each
object from `objects_by_id`, and republishes the resulting JSON
documents to `ontology.reindex.v1` with the same payload shape that
the live `ontology-indexer` consumer already understands.
